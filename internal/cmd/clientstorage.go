package cmd

import (
	"log/slog"
	"net/netip"
	"time"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/client"
	"github.com/AdguardTeam/AdGuardDNSCLI/internal/dnssvc"
	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/timeutil"
)

// defaultClientValidityIvl is the default interval for which a client
// identified by [client.DefaultHumanIDSource] is considered valid.
const defaultClientValidityIvl = 1 * time.Hour

// newClientStorage creates a new implementation of the [client.Storage]
// interface.  All arguments must not be nil.
func newClientStorage(
	baseLogger *slog.Logger,
	private *proxy.UpstreamConfig,
	static map[netip.Prefix]*proxy.UpstreamConfig,
	autodevice map[netip.Prefix]client.AutodeviceClientConfig,
	cacheConf *cacheConfig,
) (s client.Storage) {
	conf := cacheConf.toInternal()
	staticClients := newStaticClients(static, conf)

	var humanIDSrc client.HumanIDSource = client.EmptyHumanIDSource{}
	if len(autodevice) > 0 {
		rdnsSrc := client.NewRDNSIDSource(&client.RDNSIDSourceConfig{
			Clock:          timeutil.SystemClock{},
			UpstreamConfig: private,
		})
		defaultSrc := client.NewDefaultHumanIDSource(&client.DefaultHumanIDSourceConfig{
			Clock:       timeutil.SystemClock{},
			ValidityIvl: defaultClientValidityIvl,
		})

		humanIDSrc = client.ConsequentHumanIDSource{
			rdnsSrc,
			defaultSrc,
		}
	}

	clientStrgConf := &client.DefaultStorageConfig{
		Logger:        baseLogger.With(slogutil.KeyPrefix, "client_storage"),
		Clock:         timeutil.SystemClock{},
		Static:        staticClients,
		HumanIDSource: humanIDSrc,
		Autodevice:    autodevice,
		CacheEnabled:  conf.Enabled,
		CacheSize:     conf.Size,
	}

	return client.NewDefaultStorage(clientStrgConf)
}

// newStaticClients creates a list of clients from confs.  cacheConf must not
// be nil.
func newStaticClients(
	static map[netip.Prefix]*proxy.UpstreamConfig,
	cacheConf *dnssvc.CacheConfig,
) (clients map[netip.Prefix]*client.StaticClient) {
	clients = make(map[netip.Prefix]*client.StaticClient, len(static))

	for cli, conf := range static {
		cliConf := proxy.NewCustomUpstreamConfig(
			conf,
			cacheConf.Enabled,
			cacheConf.ClientSize,
			false,
		)

		clients[cli] = client.NewStaticClient(cliConf)
	}

	return clients
}
