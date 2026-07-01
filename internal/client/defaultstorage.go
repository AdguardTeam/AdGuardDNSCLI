package client

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/service"
	"github.com/AdguardTeam/golibs/syncutil"
	"github.com/AdguardTeam/golibs/timeutil"
)

// unit is a convenience alias for an empty struct.
type unit = struct{}

// DefaultStorageConfig is a configuration structure for [DefaultStorage].
type DefaultStorageConfig struct {
	// Logger is used for logging storage operations.  It must not be nil.
	Logger *slog.Logger

	// Static is a mapping of IP prefixes to clients that are known in advance.
	// Each key and value must be valid.  Prefixes must not overlap.
	//
	// TODO(e.burkov):  Consider initializing the upstreams in this package,
	// instead of passing them from the outside.
	Static map[netip.Prefix]*StaticClient

	// HumanIDSource is used to identify dynamically created clients.  It must
	// not be nil, use [EmptyHumanIDSource] if no identification is needed.
	HumanIDSource HumanIDSource

	// Autodevice is a mapping of IP prefixes to configurations of clients that
	// should be created automatically on demand.  Empty prefix defines a
	// default configuration for all addresses that are not covered by other
	// prefixes, all of which must be valid and must not overlap.  Each value
	// must be valid.
	Autodevice map[netip.Prefix]AutodeviceClientConfig

	// Clock is used to get the current time and run timers.  It must not be
	// nil.
	Clock timeutil.ClockAfter

	// CleanupIvl is the interval at which expired clients are cleaned up.  It
	// must be positive if Autodevice is not empty.
	CleanupIvl time.Duration

	// CacheEnabled controls whether dynamically created custom upstream configs
	// get their own cache.
	CacheEnabled bool

	// CacheSize is the size of the dynamically created custom upstream cache.
	CacheSize int
}

// DefaultStorage is a default implementation of the [Storage] interface.
type DefaultStorage struct {
	clock timeutil.ClockAfter

	humanIDSource HumanIDSource

	logger *slog.Logger

	searchQueue *syncutil.Map[netip.Addr, *searchResult]

	// mu protects clients.
	mu *sync.RWMutex

	cleanupDone chan unit
	autodevice  []*autodeviceConfig

	// clients stores known clients.  It must only be accessed under mu and keep
	// sorted by prefix.
	clients []*storedClient

	cleanupIvl time.Duration

	cacheSize    int
	cacheEnabled bool
}

// NewDefaultStorage creates a new properly configured *DefaultStorage.  c must
// be valid.
func NewDefaultStorage(c *DefaultStorageConfig) (s *DefaultStorage) {
	clients := make([]*storedClient, 0, len(c.Static))

	for prefix, client := range c.Static {
		cl := &storedClient{
			prefix:     prefix,
			client:     client,
			validUntil: time.Time{},
		}

		clients = append(clients, cl)
	}
	slices.SortStableFunc(clients, (*storedClient).compare)

	autodevice := make([]*autodeviceConfig, 0, len(c.Autodevice))
	for prefix, conf := range c.Autodevice {
		autodevice = append(autodevice, &autodeviceConfig{
			prefix:       prefix,
			conf:         conf,
			cacheSize:    c.CacheSize,
			cacheEnabled: c.CacheEnabled,
		})
	}
	slices.SortStableFunc(autodevice, (*autodeviceConfig).compare)

	return &DefaultStorage{
		clock:         c.Clock,
		logger:        c.Logger,
		humanIDSource: c.HumanIDSource,
		cacheEnabled:  c.CacheEnabled,
		cacheSize:     c.CacheSize,
		searchQueue:   syncutil.NewMap[netip.Addr, *searchResult](),
		mu:            &sync.RWMutex{},
		clients:       clients,
		cleanupIvl:    c.CleanupIvl,
		cleanupDone:   make(chan unit),
		autodevice:    autodevice,
	}
}

// type check
var _ Storage = (*DefaultStorage)(nil)

// ByAddr implements the [Storage] interface for *DefaultStorage.
func (d *DefaultStorage) ByAddr(ctx context.Context, addr netip.Addr) (c Client, ok bool) {
	// TODO(e.burkov):  Forbid mapped addresses by contract in [Storage].
	addr = addr.Unmap()

	cli, err := d.queue(ctx, addr)
	if err != nil {
		d.logger.DebugContext(ctx, "queuing client", "addr", addr, slogutil.KeyError, err)

		return nil, false
	} else if cli != nil {
		return cli, true
	}
	defer func() { d.done(addr, c, err) }()

	if c, ok = d.findValidClient(addr); ok {
		return c, true
	}

	for _, cli := range d.autodevice {
		if cli.prefix != (netip.Prefix{}) && !cli.prefix.Contains(addr) {
			continue
		}

		c, err = d.newAutodeviceClient(ctx, addr, cli)
		if err != nil {
			d.logger.ErrorContext(ctx, "initializing client", "addr", addr, slogutil.KeyError, err)

			return nil, false
		}

		return c, true
	}

	err = errors.ErrNoValue
	d.logger.DebugContext(ctx, "searching client", "addr", addr, slogutil.KeyError, err)

	return nil, false
}

// type check
var _ service.Interface = (*DefaultStorage)(nil)

// Start implements the [osservice.Interface] interface for *DefaultStorage.
func (d *DefaultStorage) Start(ctx context.Context) (err error) {
	if len(d.autodevice) > 0 {
		go d.sanitize(ctx)
	}

	return nil
}

// Shutdown implements the [service.Shutdowner] interface for *DefaultStorage.
func (d *DefaultStorage) Shutdown(ctx context.Context) (err error) {
	close(d.cleanupDone)

	d.mu.Lock()
	defer d.mu.Unlock()

	var errs []error

	for _, c := range d.clients {
		conf := c.client.Upstreams()
		err = conf.Close()
		if err != nil {
			err = fmt.Errorf("closing upstreams for clients from %s subnet: %w", c.prefix, err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// findValidClient finds a valid client for addr.  It returns false, if there is
// no such client or it is no longer valid.
func (d *DefaultStorage) findValidClient(addr netip.Addr) (c Client, ok bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	now := d.clock.Now()

	for _, cli := range d.clients {
		if !cli.prefix.Contains(addr) {
			continue
		}

		if !cli.isStillValid(now) {
			// The client is no longer valid, reinitialize it.
			return nil, false
		}

		return cli.client, true
	}

	return nil, false
}

// newAutodeviceClient initializes an autodevice client for the given address
// and configuration.
func (d *DefaultStorage) newAutodeviceClient(
	ctx context.Context,
	addr netip.Addr,
	c *autodeviceConfig,
) (cli Client, err error) {
	id, err := d.humanIDSource.Identify(ctx, addr)
	if err != nil {
		return nil, err
	}

	cli, err = newAutodeviceClient(id.ID, c)
	if err != nil {
		return nil, fmt.Errorf("creating autodevice client for %q: %w", addr, err)
	}

	sc := &storedClient{
		client: cli,
		// For autodevice clients use the exact address as the prefix, so that
		// it is not used for other addresses.
		prefix:     netip.PrefixFrom(addr, addr.BitLen()),
		validUntil: id.Until,
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	idx, ok := slices.BinarySearchFunc(d.clients, sc, (*storedClient).compare)
	if !ok {
		d.clients = slices.Insert(d.clients, idx, sc)
	} else {
		d.clients[idx] = sc
	}

	return cli, nil
}

// searchResult is a result of searching for a client by address.  It is used to
// deduplicate concurrent searches for the same address.
type searchResult struct {
	// finished is closed when the search is finished, and cli and err are set.
	finished chan unit
	cli      Client
	err      error
}

// queue adds a search request for addr to the queue, if another search for the
// same address is already in progress and returns the result of the first
// search, blocking until it is finished.  If the search for addr is the first
// one, it returns nil Client and nil error, and the caller must perform the
// search and call [DefaultStorage.done] when finished.  addr must be valid.
func (d *DefaultStorage) queue(ctx context.Context, addr netip.Addr) (c Client, err error) {
	res := &searchResult{
		finished: make(chan unit),
	}

	res, loaded := d.searchQueue.LoadOrStore(addr, res)
	if loaded {
		select {
		case <-res.finished:
			return res.cli, res.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, nil
}

// done marks the search for addr finished, and sets the result to cli and err.
// addr must be valid, either cli or err must not be nil.  It must only be
// called once per addr.
func (d *DefaultStorage) done(addr netip.Addr, cli Client, err error) {
	res, ok := d.searchQueue.Load(addr)
	if !ok {
		panic(fmt.Errorf("autodevice result for %q: %w", addr, errors.ErrNoValue))
	}

	res.cli = cli
	res.err = err

	close(res.finished)
	d.searchQueue.Delete(addr)
}

// storedClient is a client stored in [DefaultStorage].
type storedClient struct {
	validUntil time.Time
	client     Client
	prefix     netip.Prefix
}

// isStillValid checks whether s is valid for now.
func (s *storedClient) isStillValid(now time.Time) (ok bool) {
	return s.validUntil.IsZero() || now.Before(s.validUntil)
}

// compare is a method for sorting stored clients by prefix.  other must not be
// nil.
func (s *storedClient) compare(other *storedClient) (res int) {
	return s.prefix.Compare(other.prefix)
}

// sanitize periodically cleans up expired clients from the storage.  It's
// intended to be run in a separate goroutine.
func (d *DefaultStorage) sanitize(ctx context.Context) {
	for {
		select {
		case now := <-d.clock.After(d.cleanupIvl):
			d.cleanExpired(ctx, now)
		case <-ctx.Done():
			d.logger.ErrorContext(ctx, "sanitizing loop finished", slogutil.KeyError, ctx.Err())

			return
		case <-d.cleanupDone:
			d.logger.DebugContext(ctx, "sanitizing loop finished")

			return
		}
	}
}

// cleanExpired removes expired clients from the storage.
func (d *DefaultStorage) cleanExpired(ctx context.Context, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.clients = slices.DeleteFunc(d.clients, func(c *storedClient) (remove bool) {
		if c.isStillValid(now) {
			return false
		}

		// Use a finalizer to close the upstreams of the expired client, so that
		// it's not closed while it's still in use by other goroutines.
		runtime.AddCleanup(c, func(uc *proxy.CustomUpstreamConfig) {
			err := uc.Close()
			if err != nil {
				d.logger.ErrorContext(ctx, "cleaning expired clients", slogutil.KeyError, err)
			}
		}, c.client.Upstreams())

		return true
	})
}
