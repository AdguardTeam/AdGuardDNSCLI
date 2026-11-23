package client

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/AdguardTeam/golibs/service"
	"github.com/AdguardTeam/golibs/timeutil"
	"github.com/c2h5oh/datasize"
)

// unit is a convenience alias for an empty struct.
type unit = struct{}

// DefaultStorageConfig is a configuration structure for [DefaultStorage].
type DefaultStorageConfig struct {
	// Logger is used for logging storage operations.  It must not be nil.
	Logger *slog.Logger

	// Static is a mapping of IP prefixes to clients' domain specifications that
	// are known in advance.  Each key, if not empty, and value must be valid.
	// Prefixes must not overlap.
	//
	// TODO(e.burkov):  Consider initializing the upstreams in this package,
	// instead of passing them from the outside.
	Static map[netip.Prefix]StaticClientConfig

	// HumanIDSource is used to identify dynamically created clients.  It must
	// not be nil, use [EmptyHumanIDSource] if no identification is needed.
	HumanIDSource HumanIDSource

	// UpstreamConstructor is used to construct upstreams from addresses.  It
	// must not be nil.
	UpstreamConstructor UpstreamConstructor

	// Identifiable defines the filter for addresses that should be identified
	// and turned into autodevice clients.  If Autodevice is not empty, it must
	// not be nil, use [IsIdentifiable] wrapped in [netutil.SubnetSetFunc] as a
	// sensible default.
	Identifiable netutil.SubnetSet

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
	// It must be positive if CacheEnabled is true.
	CacheSize datasize.ByteSize
}

// DefaultStorage is a default implementation of the [Storage] interface.
type DefaultStorage struct {
	clock timeutil.ClockAfter

	humanIDSource       HumanIDSource
	upstreamConstructor UpstreamConstructor
	identifiable        netutil.SubnetSet

	logger *slog.Logger

	// identifyQueue is used to avoid concurrent calls to the humanIDSource for
	// the same address.  Unlike searchQueue, it only protects the
	// identification, as the same address may be used to create multiple
	// clients for different domains.
	identifyQueue *queue[netip.Addr, *ValidHumanID]

	// searchQueue is used to avoid concurrent autodevice creation for the same
	// address and matched domain.  Unlike identifyQueue it protects whole
	// autodevice search, as each address and domain pair can only have one
	// client.
	searchQueue *queue[searchRequest, Client]

	gcDone     chan unit
	autodevice []*storedAutodeviceClient

	// mu protects identified.
	mu         *sync.Mutex
	identified map[HumanID]*identity

	// static stores known static clients.  It must be kept sorted by prefix.
	// It also must not contain empty and overlapping prefixes.
	static []*storedClient

	cleanupIvl time.Duration
}

// NewDefaultStorage creates a new properly configured *DefaultStorage.  c must
// be valid.
func NewDefaultStorage(c *DefaultStorageConfig) (s *DefaultStorage) {
	// Use the number of configured static prefixes as the initial capacity of
	// the clients slice, as it is a good enough estimate for the initial
	// capacity.
	clients := make([]*storedClient, 0, len(c.Static))

	for prefix, conf := range c.Static {
		for domain, client := range conf {
			cl := &storedClient{
				client: client,
				prefix: prefix,
				domain: domain,
			}

			clients = append(clients, cl)
		}
	}
	slices.SortStableFunc(clients, (*storedClient).compare)

	// Use the number of configured autodevice prefixes as the initial capacity
	// of the autodevice slice, as it is a good enough estimate for the initial
	// capacity.
	autodevice := make([]*storedAutodeviceClient, 0, len(c.Autodevice))
	for prefix, conf := range c.Autodevice {
		for domain, cliConf := range conf {
			autodevice = append(autodevice, &storedAutodeviceClient{
				mu:           &sync.Mutex{},
				clients:      map[netip.Addr]*autodeviceClient{},
				conf:         cliConf,
				prefix:       prefix,
				domain:       domain,
				cacheSize:    c.CacheSize,
				cacheEnabled: c.CacheEnabled,
			})
		}
	}
	slices.SortStableFunc(autodevice, (*storedAutodeviceClient).compare)

	return &DefaultStorage{
		clock:               c.Clock,
		logger:              c.Logger,
		humanIDSource:       c.HumanIDSource,
		upstreamConstructor: c.UpstreamConstructor,
		identifiable:        c.Identifiable,
		identifyQueue:       newQueue[netip.Addr, *ValidHumanID](),
		searchQueue:         newQueue[searchRequest, Client](),
		autodevice:          autodevice,
		mu:                  &sync.Mutex{},
		identified:          map[HumanID]*identity{},
		static:              clients,
		cleanupIvl:          c.CleanupIvl,
		gcDone:              make(chan unit),
	}
}

// type check
var _ Storage = (*DefaultStorage)(nil)

// Get implements the [Storage] interface for *DefaultStorage.
func (d *DefaultStorage) Get(
	ctx context.Context,
	addr netip.Addr,
	questionDomain string,
) (c Client, ok bool) {
	l := d.logger.With("addr", addr, "domain", questionDomain)
	l.DebugContext(ctx, "searching client")

	c = d.findStaticClient(addr, questionDomain)
	if c != nil {
		l.DebugContext(ctx, "found static client")

		return c, true
	}

	l.DebugContext(ctx, "no static client found")

	c = d.findAutodevice(ctx, l, addr, questionDomain)
	if c != nil {
		return c, true
	}

	return nil, false
}

// type check
var _ service.Interface = (*DefaultStorage)(nil)

// Start implements the [service.Interface] interface for *DefaultStorage.
func (d *DefaultStorage) Start(ctx context.Context) (err error) {
	if len(d.autodevice) > 0 {
		go d.runGC(ctx)
	}

	return nil
}

// Shutdown implements the [service.Interface] interface for *DefaultStorage.
func (d *DefaultStorage) Shutdown(ctx context.Context) (err error) {
	close(d.gcDone)

	var errs []error

	for _, c := range d.static {
		err = c.client.Upstreams().Close()
		if err != nil {
			err = fmt.Errorf("closing upstreams for %q and %s: %w", c.domain, c.prefix, err)
			errs = append(errs, err)
		}
	}

	for _, c := range d.autodevice {
		err = c.Close()
		if err != nil {
			err = fmt.Errorf("closing autodevice client for %q and %s: %w", c.domain, c.prefix, err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// searchRequest is a key for a search request in the queue.  It is used to
// deduplicate concurrent searches for the same address and domain.
type searchRequest struct {
	addr   netip.Addr
	domain string
}

// findStaticClient finds a static client for addr and domain.  c is nil if no
// static client exist for addr and domain.
func (d *DefaultStorage) findStaticClient(addr netip.Addr, domain string) (c Client) {
	for _, cli := range d.static {
		if !cli.matches(addr, domain) {
			continue
		}

		return cli.client
	}

	return nil
}

// findAutodevice finds an autodevice client for addr, if it is identifiable and
// matches one of the autodevice configurations.  c is nil if a Client couldn't
// be created for addr and domain.
func (d *DefaultStorage) findAutodevice(
	ctx context.Context,
	logger *slog.Logger,
	addr netip.Addr,
	domain string,
) (c Client) {
	if len(d.autodevice) == 0 {
		d.logger.Log(ctx, slogutil.LevelTrace, "no autodevice clients configured")

		return nil
	}

	if !d.identifiable.Contains(addr) {
		logger.DebugContext(ctx, "address is not identifiable")

		return nil
	}

	for _, sac := range d.autodevice {
		if !sac.matches(addr, domain) {
			continue
		}

		return d.newAutodeviceOrStored(ctx, logger, addr, sac)
	}

	logger.DebugContext(ctx, "no autodevice client found")

	return nil
}

// findValidAutodeviceClient finds a valid autodevice client for addr and sac,
// if it exists and valid at now.  logger must not be nil, sac must be valid,
// and now must not be empty.
func (d *DefaultStorage) findValidAutodeviceClient(
	ctx context.Context,
	logger *slog.Logger,
	addr netip.Addr,
	sac *storedAutodeviceClient,
	now time.Time,
) (c Client) {
	sac.mu.Lock()
	defer sac.mu.Unlock()

	auto, ok := sac.clients[addr]
	if ok {
		if auto.validUntil.After(now) {
			logger.DebugContext(ctx, "found autodevice client", "pref", sac.prefix)

			return auto
		}

		delete(sac.clients, addr)
		d.setFinalizer(ctx, auto)
	}

	return nil
}

// newAutodeviceOrStored returns an existing autodevice client for addr and
// autoConf, if it exists and is valid, or creates a new one.  It returns nil if
// the client couldn't be created.  autoConf must not be nil, and addr must be
// valid.
func (d *DefaultStorage) newAutodeviceOrStored(
	ctx context.Context,
	logger *slog.Logger,
	addr netip.Addr,
	sac *storedAutodeviceClient,
) (c Client) {
	req := searchRequest{
		addr:   addr,
		domain: sac.domain,
	}

	c, err := d.searchQueue.push(ctx, req)
	if err != nil {
		logger.DebugContext(ctx, "queuing client", slogutil.KeyError, err)

		return nil
	} else if c != nil {
		return c
	}
	defer func() { d.searchQueue.done(req, c, err) }()

	now := d.clock.Now()

	c = d.findValidAutodeviceClient(ctx, logger, addr, sac, now)
	if c != nil {
		return c
	}

	logger.DebugContext(ctx, "creating client", "pref", sac.prefix)

	c, err = d.newAutodeviceClient(ctx, logger, addr, sac, now)
	if err != nil {
		logger.ErrorContext(ctx, "initializing client", slogutil.KeyError, err)

		return nil
	}

	logger.DebugContext(ctx, "created client", "pref", sac.prefix)

	return c
}

// newAutodeviceClient initializes an autodevice client for the given address
// and configuration.  logger must not be nil, addr must be a valid unmapped
// global unicast or private IP, sac must be valid, and now must not be empty.
func (d *DefaultStorage) newAutodeviceClient(
	ctx context.Context,
	logger *slog.Logger,
	addr netip.Addr,
	sac *storedAutodeviceClient,
	now time.Time,
) (cli Client, err error) {
	hid, err := d.identify(ctx, logger, addr, now)
	if err != nil {
		return nil, fmt.Errorf("identifying client within %s: %w", sac.prefix, err)
	}

	auto, err := newAutodeviceClient(
		hid,
		sac.conf,
		d.upstreamConstructor,
		sac.cacheEnabled,
		sac.cacheSize,
	)
	if err != nil {
		return nil, fmt.Errorf("creating autodevice for %q and %s: %w", sac.domain, addr, err)
	}

	sac.mu.Lock()
	defer sac.mu.Unlock()

	sac.clients[addr] = auto

	return auto, nil
}

// identity is a helper struct that is used to deduplicate
// HumanIDSource.Identify results.
type identity = struct {
	addr netip.Addr
	hid  *ValidHumanID
}

// identify returns a *ValidHumanID for addr, making only one concurrent call to
// the underlying [HumanIDSource].  It returns an error if the identification
// fails.  logger must not be nil, addr must be a valid unmapped global unicast
// or private IP, now must not be empty.
func (d *DefaultStorage) identify(
	ctx context.Context,
	logger *slog.Logger,
	addr netip.Addr,
	now time.Time,
) (hid *ValidHumanID, err error) {
	hid, err = d.identifyQueue.push(ctx, addr)
	if err != nil {
		return nil, err
	} else if hid != nil {
		return hid, nil
	}
	defer func() { d.identifyQueue.done(addr, hid, err) }()

	ctx = slogutil.ContextWithLogger(ctx, logger)

	hid, err = d.humanIDSource.Identify(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("identifying client %q: %w", addr, err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	existing, ok := d.identified[hid.ID]
	if ok && existing.addr != addr && existing.hid.Until.After(now) {
		return nil, fmt.Errorf(
			"identifying address %s: %w: conflicts with %s",
			addr,
			errors.ErrDuplicated,
			existing.addr,
		)
	}

	d.identified[hid.ID] = &identity{
		addr: addr,
		hid:  hid,
	}

	return hid, nil
}

// runGC periodically cleans up expired clients from the storage.  It's intended
// to be run in a separate goroutine.
func (d *DefaultStorage) runGC(ctx context.Context) {
	defer slogutil.RecoverAndLog(ctx, d.logger)

	for {
		select {
		case now := <-d.clock.After(d.cleanupIvl):
			d.logger.DebugContext(ctx, "collecting garbage")

			d.cleanExpired(ctx, now)
		case <-ctx.Done():
			d.logger.ErrorContext(ctx, "garbage collection finished", slogutil.KeyError, ctx.Err())

			return
		case <-d.gcDone:
			d.logger.DebugContext(ctx, "garbage collection finished")

			return
		}
	}
}

// cleanExpired removes expired autodevice clients from the storage, according
// to now.  now must not be empty.
func (d *DefaultStorage) cleanExpired(ctx context.Context, now time.Time) {
	for _, c := range d.autodevice {
		d.cleanExpiredClients(ctx, c, now)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, id := range d.identified {
		if id.hid.Until.Before(now) {
			delete(d.identified, id.hid.ID)
		}
	}
}

// cleanExpiredClients removes expired clients from c.  c must not be nil, now
// must not be empty.
func (d *DefaultStorage) cleanExpiredClients(
	ctx context.Context,
	c *storedAutodeviceClient,
	now time.Time,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	maps.DeleteFunc(c.clients, func(_ netip.Addr, client *autodeviceClient) (ok bool) {
		if now.Before(client.validUntil) {
			return false
		}

		d.setFinalizer(ctx, client)

		return true
	})
}

// setFinalizer sets a finalizer to c's upstreams to close them when they are no
// longer used.  c must be removed from d under the same autodevice lock period.
//
// TODO(e.burkov):  Consider using [runtime.AddCleanup] instead of finalizers.
func (d *DefaultStorage) setFinalizer(ctx context.Context, c Client) {
	ctx = context.WithoutCancel(ctx)

	runtime.SetFinalizer(c.Upstreams(), func(closer *proxy.CustomUpstreamConfig) {
		d.logger.DebugContext(ctx, "cleaning expired client")

		err := closer.Close()
		if err != nil {
			d.logger.ErrorContext(ctx, "cleaning expired client", slogutil.KeyError, err)
		}
	})
}

// storedClient is a client stored in [DefaultStorage].
type storedClient struct {
	client Client

	// prefix must be valid for any client stored in [DefaultStorage].  If
	// client is [*autodeviceClient], prefix must be a single IP address.
	prefix netip.Prefix

	// domain is a non-FQDN domain name.  A question domain should be equal or a
	// subdomain of this domain to match, empty domain matches any question
	// domain.
	domain string
}

// compare is a method for sorting stored clients by prefix and domain.  other
// must not be nil.
//
// TODO(e.burkov):  DRY with [storedAutodeviceClient.compare], consider removing
// IsSingleIP check.
func (c *storedClient) compare(other *storedClient) (res int) {
	switch {
	case c.prefix == (netip.Prefix{}):
		if other.prefix == (netip.Prefix{}) {
			return compareDomains(c.domain, other.domain)
		}

		return 1
	case other.prefix == (netip.Prefix{}):
		return -1
	default:
		// Go on.
	}

	if a, b := c.prefix.IsSingleIP(), other.prefix.IsSingleIP(); a != b {
		if a {
			return -1
		}

		return 1
	}

	res = c.prefix.Compare(other.prefix)
	if res == 0 {
		return compareDomains(c.domain, other.domain)
	}

	return res
}

// compareDomains compares domains sorting more specific domains first, empty
// domains last, and otherwise comparing labels from right to left.  a and b
// must be valid non-FQDNs in the same letter case or empty.
func compareDomains(a, b string) (res int) {
	const labelSep = '.'

	for {
		if a == "" {
			if b == "" {
				return 0
			}

			return 1
		} else if b == "" {
			return -1
		}

		aLastDot := strings.LastIndexByte(a, labelSep)
		bLastDot := strings.LastIndexByte(b, labelSep)

		aLabel := a[aLastDot+1:]
		bLabel := b[bLastDot+1:]

		res = strings.Compare(aLabel, bLabel)
		if res != 0 {
			return res
		}

		a = a[:max(0, aLastDot)]
		b = b[:max(0, bLastDot)]
	}
}

// matchesDomain returns true if question is equal to configured or is its
// subdomain.  Empty configured matches any question domain.
func matchesDomain(question, configured string) (ok bool) {
	return configured == "" || question == configured || netutil.IsSubdomain(question, configured)
}

// matches returns true if c matches addr and domain pair.  addr must be valid,
// domain, if not empty, must be a valid non-FQDN.
//
// TODO(e.burkov):  DRY with [storedAutodeviceClient.matches].
func (c *storedClient) matches(addr netip.Addr, domain string) (ok bool) {
	if c.prefix != (netip.Prefix{}) && !c.prefix.Contains(addr) {
		return false
	}

	if !matchesDomain(domain, c.domain) {
		return false
	}

	return true
}
