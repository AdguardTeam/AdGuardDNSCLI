package client

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/service"
	"github.com/AdguardTeam/golibs/syncutil"
	"github.com/AdguardTeam/golibs/timeutil"
)

// DefaultStorageConfig is a configuration structure for [DefaultStorage].
type DefaultStorageConfig struct {
	// Logger is used for logging storage operations.  It must not be nil.
	Logger *slog.Logger

	// Static is a mapping of IP prefixes to clients that are known in advance.
	// Each key and value must be valid.  Subnets, which are represented by
	// prefixes, must not overlap.
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
	// prefixes, each value must be valid.
	Autodevice map[netip.Prefix]AutodeviceClientConfig

	// Clock is used to get the current time.  It must not be nil.
	Clock timeutil.Clock

	// CacheEnabled controls whether dynamically created custom upstream configs
	// get their own cache.
	CacheEnabled bool

	// CacheSize is the size of the dynamically created custom upstream cache.
	CacheSize int
}

// DefaultStorage is a default implementation of the [Storage] interface.
type DefaultStorage struct {
	clock         timeutil.Clock
	humanIDSource HumanIDSource

	logger          *slog.Logger
	pendingRequests *syncutil.Map[netip.Addr, *searchResult]

	// mu protects clients.
	mu *sync.RWMutex

	autodevice []*autodeviceConfig

	clients []*storedClient

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
		clock:           c.Clock,
		logger:          c.Logger,
		humanIDSource:   c.HumanIDSource,
		cacheEnabled:    c.CacheEnabled,
		cacheSize:       c.CacheSize,
		pendingRequests: syncutil.NewMap[netip.Addr, *searchResult](),
		mu:              &sync.RWMutex{},
		clients:         clients,
		autodevice:      autodevice,
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
		d.logger.ErrorContext(ctx, "queuing client", "addr", addr, slogutil.KeyError, err)

		return nil, false
	} else if cli != nil {
		return cli, true
	}
	defer func() { d.done(addr, c, err) }()

	if c, ok = d.findValidClient(addr); ok {
		return c, true
	}

	for _, cli := range d.autodevice {
		// TODO(e.burkov):  !! handle empty prefix
		if !cli.prefix.Contains(addr) {
			continue
		}

		c, err = d.initAutodeviceClient(ctx, addr, cli)
		if err != nil {
			d.logger.ErrorContext(ctx, "initializing autodevice client", "addr", addr, slogutil.KeyError, err)

			return nil, false
		}
	}

	return nil, false
}

// type check
var _ service.Shutdowner = (*DefaultStorage)(nil)

// Shutdown implements the [service.Shutdowner] interface for *DefaultStorage.
func (d *DefaultStorage) Shutdown(ctx context.Context) (err error) {
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

// initAutodeviceClient initializes an autodevice client for the given address
// and configuration.
func (d *DefaultStorage) initAutodeviceClient(
	ctx context.Context,
	addr netip.Addr,
	c *autodeviceConfig,
) (cli Client, err error) {
	id, err := d.humanIDSource.Identify(ctx, addr)
	if err != nil {
		return nil, err
	}

	cli = newAutodeviceClient(id.ID, c)

	sc := &storedClient{
		client:     cli,
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
	finished chan struct{}
	cli      Client
	err      error
}

// queue adds a search for addr to the queue of pending searches.  addr must be
// valid.  It returns
func (d *DefaultStorage) queue(ctx context.Context, addr netip.Addr) (c Client, err error) {
	res := &searchResult{
		finished: make(chan struct{}),
	}

	res, loaded := d.pendingRequests.LoadOrStore(addr, res)
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

// done marks the search for addr as finished, and sets the result to cli and
// err.  addr must be valid, either cli or err must not be nil.  It must only be
// called once per addr.
func (d *DefaultStorage) done(addr netip.Addr, cli Client, err error) {
	res, ok := d.pendingRequests.Load(addr)
	if !ok {
		panic(fmt.Errorf("autodevice result for %q: %w", addr, errors.ErrNoValue))
	}

	res.cli = cli
	res.err = err

	close(res.finished)
	d.pendingRequests.Delete(addr)
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
