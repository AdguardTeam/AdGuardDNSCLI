// Package dnssvc provides DNS handling functionality for AdGuard DNS CLI.
package dnssvc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/client"
	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/AdguardTeam/golibs/service"
)

// DNSService is a service that provides DNS handling functionality.
type DNSService struct {
	// logger is used as the base logger for the DNS service.
	logger *slog.Logger

	// proxy forwards DNS requests.
	proxy *proxy.Proxy

	// clientStorage stores upstream configurations associated with client
	// addresses.
	clientStorage client.Storage

	// clientGetter is used to get the client's address from the request's
	// context.  It's only used for testing.
	//
	// TODO(e.burkov):  Use custom client's address from dnsproxy context and
	// get rid of this interface.
	clientGetter ClientGetter
}

// New creates a new DNSService.  conf must not be nil.
func New(conf *Config) (svc *DNSService, err error) {
	prxConf, err := newProxyConfig(conf, conf.Bootstrap)
	if err != nil {
		// Don't wrap the error, because it's informative enough as is.
		return nil, err
	}

	svc = &DNSService{
		logger:        conf.Logger,
		clientGetter:  conf.ClientGetter,
		clientStorage: conf.ClientStorage,
	}
	prxConf.RequestHandler = svc.Wrap(svc)

	prx, err := proxy.New(prxConf)
	if err != nil {
		return nil, fmt.Errorf("creating proxy: %w", err)
	}

	svc.proxy = prx

	return svc, nil
}

// newProxyConfig creates a new [proxy.Config] from conf.  It returns a
// ready-to-use proxy configuration.  conf must not be nil.
func newProxyConfig(
	conf *Config,
	boot upstream.Resolver,
) (prxConf *proxy.Config, err error) {
	defer func() { err = errors.Annotate(err, "creating proxy configuration: %w") }()

	falls, err := newFallbacks(conf.Fallbacks, conf.Logger, boot)
	if err != nil {
		// Don't wrap the error, because it's informative enough as is.
		return nil, err
	}

	udp, tcp := newListenAddrs(conf.ListenAddrs)
	// TODO(e.burkov):  Consider making configurable.
	trusted := netutil.SliceSubnetSet{
		netip.PrefixFrom(netip.IPv4Unspecified(), 0),
		netip.PrefixFrom(netip.IPv6Unspecified(), 0),
	}

	return &proxy.Config{
		Logger:                    conf.BaseLogger.With(slogutil.KeyPrefix, "dnsproxy"),
		UpstreamMode:              proxy.UpstreamModeLoadBalance,
		UDPListenAddr:             udp,
		TCPListenAddr:             tcp,
		UpstreamConfig:            conf.GeneralUpstreams,
		PrivateRDNSUpstreamConfig: conf.PrivateRDNSUpstreams,
		PrivateSubnets:            conf.PrivateSubnets,
		UsePrivateRDNS:            conf.PrivateRDNSUpstreams != nil,
		Fallbacks:                 falls,
		TrustedProxies:            trusted,
		CacheSizeBytes:            conf.Cache.Size,
		CacheEnabled:              conf.Cache.Enabled,
		BindRetryConfig: &proxy.BindRetryConfig{
			Enabled:  conf.BindRetry.Enabled,
			Interval: conf.BindRetry.Interval,
			Count:    conf.BindRetry.Count,
		},
		PendingRequests: &proxy.PendingRequestsConfig{
			Enabled: conf.PendingRequests.Enabled,
		},
	}, nil
}

// newListenAddrs creates a new list of UDP and TCP addresses from addrs.
//
// TODO(e.burkov):  Support other protos.
func newListenAddrs(addrs []netip.AddrPort) (udp []*net.UDPAddr, tcp []*net.TCPAddr) {
	udp = make([]*net.UDPAddr, 0, len(addrs))
	tcp = make([]*net.TCPAddr, 0, len(addrs))
	for _, addr := range addrs {
		udp = append(udp, net.UDPAddrFromAddrPort(addr))
		tcp = append(tcp, net.TCPAddrFromAddrPort(addr))
	}

	return udp, tcp
}

// type check
var _ service.Interface = (*DNSService)(nil)

// Start implements the [service.Interface] interface for *DNSService.
func (svc *DNSService) Start(ctx context.Context) (err error) {
	svc.logger.DebugContext(ctx, "starting")

	return svc.proxy.Start(ctx)
}

// Shutdown implements the [service.Interface] interface for *DNSService.
func (svc *DNSService) Shutdown(ctx context.Context) (err error) {
	svc.logger.DebugContext(ctx, "shutting down")

	err = svc.proxy.Shutdown(ctx)
	if err != nil {
		return fmt.Errorf("stopping proxy: %w", err)
	}

	return nil
}

// type check
var _ proxy.Middleware = (*DNSService)(nil)

// Wrap implements the [proxy.Middleware] interface for *DNSService.
func (svc *DNSService) Wrap(h proxy.Handler) (wrapped proxy.Handler) {
	f := func(ctx context.Context, p *proxy.Proxy, dctx *proxy.DNSContext) (err error) {
		// This is used to substitute the client's address in tests.
		dctx.Addr = svc.clientGetter.Address(dctx)

		// Check the address privateness because proxy does it before
		// the substitution.  See TODO on [DNSService.clientGetter].
		dctx.IsPrivateClient = svc.proxy.PrivateSubnets.Contains(dctx.Addr.Addr())

		return h.ServeDNS(ctx, p, dctx)
	}

	return proxy.HandlerFunc(f)
}

// type check
var _ proxy.Handler = (*DNSService)(nil)

// ServeDNS implements the [proxy.Handler] interface for *DNSService.
func (svc *DNSService) ServeDNS(
	ctx context.Context,
	p *proxy.Proxy,
	dctx *proxy.DNSContext,
) (err error) {
	if dctx.RequestedPrivateRDNS != (netip.Prefix{}) {
		// Don't match client for private PTR request.
		return p.Resolve(ctx, dctx)
	}

	ctx = slogutil.ContextWithLogger(ctx, svc.logger)

	c, ok := svc.clientStorage.ByAddr(ctx, dctx.Addr.Addr())
	if ok {
		dctx.CustomUpstreamConfig = c.Upstreams()
	}

	return p.Resolve(ctx, dctx)
}
