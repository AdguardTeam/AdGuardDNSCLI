package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/client"
	"github.com/AdguardTeam/AdGuardDNSCLI/internal/dnssvc"
	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/container"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/AdguardTeam/golibs/validate"
)

// dnsConfig is the configuration for handling DNS.
type dnsConfig struct {
	// Cache configures the DNS results cache.
	Cache *cacheConfig `yaml:"cache"`

	// Server configures handling of incoming DNS requests.
	Server *serverConfig `yaml:"server"`

	// Bootstrap configures the resolving of upstream's hostnames.
	Bootstrap *bootstrapConfig `yaml:"bootstrap"`

	// Upstream configures the DNS upstream servers.
	Upstream *upstreamConfig `yaml:"upstream"`

	// Fallback configures the fallback DNS upstream servers.
	Fallback *fallbackConfig `yaml:"fallback"`
}

// type check
var _ validate.Interface = (*dnsConfig)(nil)

// Validate implements the [validate.Interface] interface for *dnsConfig.
func (c *dnsConfig) Validate() (err error) {
	if c == nil {
		return errors.ErrNoValue
	}

	validators := container.KeyValues[string, validate.Interface]{{
		Key:   "cache",
		Value: c.Cache,
	}, {
		Key:   "server",
		Value: c.Server,
	}, {
		Key:   "bootstrap",
		Value: c.Bootstrap,
	}, {
		Key:   "upstream",
		Value: c.Upstream,
	}, {
		Key:   "fallback",
		Value: c.Fallback,
	}}

	var errs []error
	for _, v := range validators {
		errs = validate.Append(errs, v.Key, v.Value)
	}

	return errors.Join(errs...)
}

// toInternal converts the DNS configuration to the internal representation.  c
// must be valid.  baseLogger, clientStorage and general must not be nil.
func (c *dnsConfig) toInternal(
	baseLogger *slog.Logger,
	clientStorage client.Storage,
	general *proxy.UpstreamConfig,
	private *proxy.UpstreamConfig,
	boot upstream.Resolver,
) (conf *dnssvc.Config) {
	listenAddrs := make([]netip.AddrPort, 0, len(c.Server.ListenAddresses))
	for _, s := range c.Server.ListenAddresses {
		listenAddrs = append(listenAddrs, s.Address)
	}

	return &dnssvc.Config{
		BaseLogger:       baseLogger,
		ClientStorage:    clientStorage,
		GeneralUpstreams: general,
		Logger:           baseLogger.With(slogutil.KeyPrefix, "dnssvc"),
		// TODO(e.burkov):  Consider making configurable.
		PrivateSubnets:       netutil.SubnetSetFunc(netutil.IsLocallyServed),
		PrivateRDNSUpstreams: private,
		Bootstrap:            boot,
		Cache:                c.Cache.toInternal(),
		Fallbacks:            c.Fallback.toInternal(),
		ClientGetter:         dnssvc.DefaultClientGetter{},
		ListenAddrs:          listenAddrs,
		BindRetry:            c.Server.BindRetry.toInternal(),
		PendingRequests:      c.Server.PendingRequests.toInternal(),
	}
}

// ipPortConfig is the object for configuring an entity having an IP address
// with a port.
type ipPortConfig struct {
	// Address is the address of the server.
	Address netip.AddrPort `yaml:"address"`
}

// type check
var _ validate.Interface = (*ipPortConfig)(nil)

// Validate implements the [validate.Interface] interface for *ipPortConfig.
func (c *ipPortConfig) Validate() (err error) {
	if c == nil {
		return errors.ErrNoValue
	}

	return validate.NotEmpty("address", c.Address)
}

// initDNSService creates and starts the DNS service.  c, baseLogger, and
// svcHdlr must not be nil.
func initDNSService(
	ctx context.Context,
	c *dnsConfig,
	baseLogger *slog.Logger,
	svcHdlr *serviceHandler,
) (err error) {
	dnsConf, err := newDNSServiceConfig(c, baseLogger, svcHdlr)
	if err != nil {
		// Don't wrap the error, because it is informative enough as is.
		return err
	}

	dnsSvc, err := dnssvc.New(dnsConf)
	if err != nil {
		return fmt.Errorf("creating dns service: %w", err)
	}

	err = dnsSvc.Start(ctx)
	if err != nil {
		return fmt.Errorf("starting dns service: %w", err)
	}

	svcHdlr.add(dnsSvc)

	return nil
}

// newDNSServiceConfig builds a new DNS configuration.  c, baseLogger, and
// svcHdlr must not be nil.
func newDNSServiceConfig(
	c *dnsConfig,
	baseLogger *slog.Logger,
	svcHdlr *serviceHandler,
) (conf *dnssvc.Config, err error) {
	boot, closers, err := newResolvers(c.Bootstrap, baseLogger)
	if err != nil {
		return nil, fmt.Errorf("creating resolvers: %w", err)
	}

	svcHdlr.add(closers)

	var (
		general    = &proxy.UpstreamConfig{}
		private    = &proxy.UpstreamConfig{}
		static     = make(map[netip.Prefix]*proxy.UpstreamConfig)
		autodevice = make(map[netip.Prefix]client.AutodeviceClientConfig)
	)

	err = classifyUpstreams(c.Upstream, baseLogger, boot, general, private, static, autodevice)
	if err != nil {
		return nil, fmt.Errorf("classifying upstreams: %w", err)
	}

	cs := newClientStorage(baseLogger, private, static, autodevice, c.Cache)

	svcHdlr.add(cs)

	dnsConf := c.toInternal(baseLogger, cs, general, private, boot)

	return dnsConf, nil
}
