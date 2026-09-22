package client

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/AdguardTeam/golibs/timeutil"
	"github.com/AdguardTeam/golibs/validate"
	"github.com/miekg/dns"
	"github.com/patrickmn/go-cache"
)

const (
	// defaultCacheTTL is a default cache TTL.
	//
	// TODO(m.kazantsev):  This is used as a default negative answer caching
	// TTL.  Use the TTL from responses for that.
	defaultCacheTTL = 5 * time.Minute

	// defaultCacheCleanupIvl is a default cache cleanup interval.
	//
	// TODO(e.burkov):  Consider making configurable.
	defaultCacheCleanupIvl = 1 * time.Minute
)

// RDNSIDSourceConfig is the configuration for [RDNSIDSource].
type RDNSIDSourceConfig struct {
	// Clock is used for determining the validity of IDs.  It must not be nil.
	Clock timeutil.Clock

	// UpstreamConfig is the configuration for the upstream resolver used for
	// reverse DNS lookups.  It must be valid according to
	// [proxy.UpstreamConfig.ValidatePrivate].
	UpstreamConfig *proxy.UpstreamConfig
}

// RDNSIDSource is an [HumanIDSource] that assigns HumanIDs based on the
// hostname obtained from reverse DNS lookups of IP addresses.
type RDNSIDSource struct {
	ups *proxy.UpstreamConfig

	clock timeutil.Clock

	// mu protects existingIDs.
	mu *sync.Mutex

	// existingIDs stores HumanID to avoid duplicated IDs.  The values are the
	// time until which the tracked HumanID is valid.
	existingIDs map[HumanID]time.Time

	// cache is a key-value storage in which the key must be a string
	// representation of [netip.Addr] and the value must be of type
	// *ValidHumanID.
	cache *cache.Cache
}

// NewRDNSIDSource returns properly initialized *RDNSIDSource.  conf must be
// non-nil and valid.
func NewRDNSIDSource(conf *RDNSIDSourceConfig) (r *RDNSIDSource) {
	r = &RDNSIDSource{
		clock: conf.Clock,
		ups:   conf.UpstreamConfig,
		// TODO(m.kazantsev):  Consider making the cleanup interval
		// configurable.
		//
		// TODO(m.kazantsev):  Add default expiration time for negative answers.
		cache:       cache.New(0, defaultCacheCleanupIvl),
		mu:          &sync.Mutex{},
		existingIDs: map[HumanID]time.Time{},
	}

	r.cache.OnEvicted(func(s string, v any) {
		r.mu.Lock()
		defer r.mu.Unlock()

		id, ok := v.(*ValidHumanID)
		if !ok {
			panic(fmt.Errorf("unexpected type of cache item by key %s: %T(%[1]v)", s, v))
		}

		if id != nil {
			delete(r.existingIDs, id.ID)
		}
	})

	return r
}

// type check
var _ HumanIDSource = (*RDNSIDSource)(nil)

// Identify implements the [HumanIDSource] interface for *RDNSIDSource.  ctx
// must contain logger accessible with [slogutil.LoggerFromContext].
func (r *RDNSIDSource) Identify(
	ctx context.Context,
	addr netip.Addr,
) (id *ValidHumanID, err error) {
	id, ok := r.replyFromCache(addr)
	if ok {
		if id == nil {
			return nil, errors.Error("no valid response was received from dns")
		}

		return id, nil
	}

	l := slogutil.MustLoggerFromContext(ctx)

	resp, err := r.sendDNSRequest(ctx, l, addr)
	if err != nil {
		// Don't wrap the error, because it is informative enough as is.
		return nil, err
	}

	return r.humanIDFromResponse(ctx, l, resp, addr)
}

// humanIDFromResponse extracts a [HumanID] from the PTR response by addr and
// returns it wrapped within ValidHumanID.  l and resp must not be nil, addr
// must be a valid unmapped global unicast or private IP.
func (r *RDNSIDSource) humanIDFromResponse(
	ctx context.Context,
	l *slog.Logger,
	resp *dns.Msg,
	addr netip.Addr,
) (id *ValidHumanID, err error) {
	var errs []error

	now := r.clock.Now()
	for i, ans := range resp.Answer {
		ptr, ok := ans.(*dns.PTR)
		if !ok {
			continue
		}

		var hid HumanID
		var ttl time.Duration
		hid, ttl, err = r.humanIDFromPTR(ptr, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("answer: at index %d: %w", i, err))

			continue
		}

		id = &ValidHumanID{
			ID:    hid,
			Until: now.Add(ttl),
		}

		r.cache.Set(addr.String(), id, ttl)

		return id, nil
	}

	l.ErrorContext(ctx, "no valid ptr dns responses were received", "addr", addr)

	r.cache.Set(addr.String(), (*ValidHumanID)(nil), defaultCacheTTL)

	if err = errors.Join(errs...); err == nil {
		err = errors.ErrNoValue
	}

	return nil, fmt.Errorf("ptr responses for %s: %w", addr, err)
}

// humanIDFromPTR converts a PTR record into a ValidHumanID.  ptr must not be
// nil.
func (r *RDNSIDSource) humanIDFromPTR(
	ptr *dns.PTR,
	now time.Time,
) (id HumanID, ttl time.Duration, err error) {
	ttl = time.Duration(ptr.Hdr.Ttl) * time.Second
	err = validate.NotEmpty("ptr ttl", ttl)
	if err != nil {
		// Don't wrap the error, because it is informative enough as is.
		return "", 0, err
	}

	id, err = fqdnToHumanID(ptr.Ptr)
	if err != nil {
		return "", 0, fmt.Errorf("converting fqdn: %w", err)
	}

	err = r.checkDuplicates(id, now, ttl)
	if err != nil {
		return "", 0, fmt.Errorf("human id %q: %w", id, err)
	}

	return id, ttl, nil
}

// checkDuplicates checks if id has already been indexed and is valid for now.
// If it is, the [errors.ErrDuplicated] error is returned.  Otherwise, id is
// indexed for the duration of ttl since now.
func (r *RDNSIDSource) checkDuplicates(id HumanID, now time.Time, ttl time.Duration) (err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if until, ok := r.existingIDs[id]; ok && until.After(now) {
		return errors.ErrDuplicated
	}

	r.existingIDs[id] = now.Add(ttl)

	return nil
}

// sendDNSRequest sends a DNS request to resolve addr.  l must not be nil.
func (r *RDNSIDSource) sendDNSRequest(
	ctx context.Context,
	l *slog.Logger,
	addr netip.Addr,
) (resp *dns.Msg, err error) {
	// TODO(m.kazantsev):  Add a helper which accepts [netip.Addr] to golibs.
	rAddr, err := netutil.IPToReversedAddr(net.IP(addr.AsSlice()))
	if err != nil {
		l.ErrorContext(ctx, "failed to reverse addr", "addr", addr, slogutil.KeyError, err)

		return nil, fmt.Errorf("reversing addr: %w", err)
	}

	req := &dns.Msg{
		Id:       dns.Id(),
		Compress: true,
		Question: []dns.Question{{
			Name:   dns.Fqdn(rAddr),
			Qtype:  dns.TypePTR,
			Qclass: dns.ClassINET,
		}},
	}

	// TODO(m.kazantsev):  Export the [proxy.UpstreamConfig] methods for
	// choosing upstreams.
	resp, ups, err := upstream.ExchangeParallel(r.ups.Upstreams, req)
	if err != nil {
		l.ErrorContext(ctx, "sending ptr dns request", slogutil.KeyError, err)

		return nil, fmt.Errorf("sending ptr dns request: %w", err)
	}

	l.DebugContext(ctx, "rdns response upstream", "upstream", ups.Address(), "rcode", resp.Rcode)

	err = validate.Equal("response code", resp.Rcode, dns.RcodeSuccess)
	if err != nil {
		r.cache.Set(addr.String(), (*ValidHumanID)(nil), defaultCacheTTL)

		rCodeStr := dns.RcodeToString[resp.Rcode]

		l.ErrorContext(ctx, "bad resp rcode", slogutil.KeyError, err, "rcode", rCodeStr)

		// Don't wrap the error, because it is informative enough as is.
		return nil, err
	}

	return resp, nil
}

// replyFromCache gets a value from cache by addr.  addr must be a valid
// unmapped global unicast or private IP.
func (r *RDNSIDSource) replyFromCache(addr netip.Addr) (id *ValidHumanID, ok bool) {
	key := addr.String()
	value, ok := r.cache.Get(key)
	if !ok {
		return nil, false
	}

	id, ok = value.(*ValidHumanID)
	if !ok {
		panic(fmt.Errorf("unexpected type of cache item: %T(%[1]v)", value))
	}

	return id, true
}
