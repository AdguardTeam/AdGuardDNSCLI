package cmd

import (
	"log/slog"
	"maps"
	"net/netip"
	"slices"
	"time"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/client"
	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/AdguardTeam/golibs/timeutil"
)

// defaultClientCleanupIvl is the default interval to run the client storage
// cleanup.
//
// TODO(e.burkov):  Make configurable.
const defaultClientCleanupIvl = 5 * time.Minute

// defaultClientValidityIvl is the default interval for which a client
// identified by [client.DefaultHumanIDSource] is considered valid.
const defaultClientValidityIvl = 1 * time.Hour

// staticUpstreams is a set of upstream configurations mapped to question
// domains.  Its keys should be valid non-FQDNs, and its values must not be nil.
type staticUpstreams map[string]*proxy.UpstreamConfig

// staticUpstreamConfigs is a set of client-specific upstream configurations
// mapped to subnets.  Its keys, if not empty, should be valid prefixes, and its
// values must be valid.
type staticUpstreamConfigs map[netip.Prefix]staticUpstreams

// staticClients is a set of static clients mapped to their subnets.  Its keys,
// if not empty, must be valid prefixes, and its values must be valid.
type staticClients map[netip.Prefix]client.StaticClientConfig

// autodeviceClients is a set of autodevice clients mapped to their subnets.
// Its keys, if not empty, must be valid prefixes, and its values must be valid.
type autodeviceClients map[netip.Prefix]client.AutodeviceClientConfig

// newClientStorage creates a [client.Storage].  All arguments must not be nil.
func newClientStorage(
	baseLogger *slog.Logger,
	privateUps *proxy.UpstreamConfig,
	staticUps staticUpstreamConfigs,
	autodeviceUps autodeviceClients,
	cacheConf *cacheConfig,
) (s client.Storage) {
	staticCli := staticUps.newStaticClients(cacheConf)

	var humanIDSrc client.HumanIDSource = client.EmptyHumanIDSource{}
	if len(autodeviceUps) > 0 {
		humanIDSrc = newHumanIDSource(privateUps)
	}

	clientStrgConf := &client.DefaultStorageConfig{
		Logger:              baseLogger.With(slogutil.KeyPrefix, "client_storage"),
		Clock:               timeutil.SystemClock{},
		Static:              staticCli,
		HumanIDSource:       humanIDSrc,
		Autodevice:          autodeviceUps,
		UpstreamConstructor: client.DefaultUpstreamConstructor{},
		// TODO(e.burkov):  Consider making configurable.
		Identifiable: netutil.SubnetSetFunc(client.IsIdentifiable),
		CleanupIvl:   defaultClientCleanupIvl,
		CacheSize:    cacheConf.ClientSize,
		CacheEnabled: cacheConf.Enabled,
	}

	return client.NewDefaultStorage(clientStrgConf)
}

// newHumanIDSource creates a [client.HumanIDSource] based on the given private
// upstream configuration.  If private has no upstreams, the returned source
// won't use it.  private must not be nil.
func newHumanIDSource(private *proxy.UpstreamConfig) (hs client.HumanIDSource) {
	defaultSrc := client.NewDefaultHumanIDSource(&client.DefaultHumanIDSourceConfig{
		Clock:       timeutil.SystemClock{},
		ValidityIvl: defaultClientValidityIvl,
	})

	if len(private.Upstreams) == 0 {
		return defaultSrc
	}

	rdnsSrc := client.NewRDNSIDSource(&client.RDNSIDSourceConfig{
		Clock:          timeutil.SystemClock{},
		UpstreamConfig: private,
	})

	return client.ConsequentHumanIDSource{
		rdnsSrc,
		defaultSrc,
	}
}

// newStaticClients creates a set of static clients based on upsConfs.  c must
// not be nil.
func (upsConfs staticUpstreamConfigs) newStaticClients(c *cacheConfig) (clients staticClients) {
	clients = make(staticClients, len(upsConfs))

	for _, pref := range slices.SortedFunc(maps.Keys(upsConfs), netip.Prefix.Compare) {
		for _, domain := range slices.Sorted(maps.Keys(upsConfs[pref])) {
			clients.addStaticClient(pref, domain, upsConfs[pref][domain], c)
		}
	}

	return clients
}

// addStaticClient adds a static client for the given subnet and domain to cs.
// conf must not be nil, and c must not be nil.  pref must be a valid subnet or
// empty, and domain must be a valid non-FQDN.
func (cs staticClients) addStaticClient(
	pref netip.Prefix,
	domain string,
	conf *proxy.UpstreamConfig,
	c *cacheConfig,
) {
	upsConf := proxy.NewCustomUpstreamConfig(conf, c.Enabled, c.ClientSize, false)

	cliConf, ok := cs[pref]
	if !ok {
		cliConf = client.StaticClientConfig{}
		cs[pref] = cliConf
	}

	cliConf[domain] = client.NewStaticClient(upsConf)
}
