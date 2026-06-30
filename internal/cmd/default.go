package cmd

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"time"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/agdc"
	"github.com/AdguardTeam/AdGuardDNSCLI/internal/configmigrate"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/AdguardTeam/golibs/timeutil"
	"github.com/c2h5oh/datasize"
	"gopkg.in/yaml.v3"
)

// Values for the default DNS configuration.
const (
	// defaultUpstreamAddress is the address of common upstream DNS server to
	// use by default.
	defaultUpstreamAddress = "https://unfiltered.adguard-dns.com/dns-query"

	// defaultFallbackAddress is the address of the fallback upstream DNS server
	// to use by default.
	defaultFallbackAddress = "tls://94.140.14.140"

	// defaultPlainDNSPort is the default port for plain DNS.
	defaultPlainDNSPort uint16 = 53

	// defaultUpstreamTimeout is the default timeout for outgoing DNS requests.
	defaultUpstreamTimeout = 2 * time.Second
)

// Values for the default server configuration.
const (
	// defaultBindRetryEnabled is the default value for the bind retrying
	// feature to be enabled.
	//
	// See https://github.com/AdguardTeam/AdGuardDNSCLI/issues/11.
	defaultBindRetryEnabled = true

	// defaultBindRetryIvl is the default interval to wait between listen
	// addresses binding retries.
	defaultBindRetryIvl = 1 * time.Second

	// defaultBindRetryCount is the default maximum number of attempts to bind a
	// listen address after the first one.
	defaultBindRetryCount uint = 4

	// defaultPendingRequestsEnabled is the default value for the pending
	// requests feature to be enabled.
	defaultPendingRequestsEnabled = true
)

// Values for the default cache configuration.
const (
	// defaultCacheEnabled is the default value for the cache usage.
	defaultCacheEnabled = true

	// defaultCacheSize is the default size of the cache.
	defaultCacheSize = 128 * datasize.MB

	// defaultCacheClientSize is the default size of the cache for the client.
	defaultCacheClientSize = 4 * datasize.MB
)

// Values for the default profiling configuration.
const (
	// defaultPprofEnabled is the default value for the pprof server to be
	// enabled.
	defaultPprofEnabled = false

	// defaultPprofPort is the default port to serve pprof handlers locally.
	defaultPprofPort uint16 = 6060
)

// Values for the default log configuration.
const (
	// defaultLogOutput is the default output for the logs.
	defaultLogOutput = outputSyslog

	// defaultLogFormat is the default format for the logs.
	defaultLogFormat = slogutil.FormatDefault

	// defaultLogTimestamp is the default value for the timestamp presence in
	// logs.
	defaultLogTimestamp = false

	// defaultLogVerbose is the default value for the verbosity of the logs.
	defaultLogVerbose = false
)

// filterInterfaceAddrs gets the addresses as given by [net.InterfaceAddrs] and
// filters out the ones that are not in the set.  It returns the [ipPortConfig]s
// for the eligible addresses created using port p.
//
// TODO(e.burkov):  Use logger instead of [fmt.Fprintf].
func filterInterfaceAddrs(
	addrs []net.Addr,
	set netutil.SubnetSet,
	p uint16,
) (confs []*ipPortConfig) {
	for _, a := range addrs {
		addrStr := a.String()
		pref, err := netip.ParsePrefix(addrStr)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "unexpected %q format: %s\n", addrStr, err)

			continue
		}

		addr := pref.Addr()
		if !set.Contains(addr) {
			_, _ = fmt.Fprintf(os.Stderr, "can not listen on %s\n", addr)

			continue
		}

		confs = append(confs, &ipPortConfig{
			Address: netip.AddrPortFrom(addr, p),
		})

		_, _ = fmt.Fprintf(os.Stderr, "adding %s to default listening addresses\n", addr)
	}

	return confs
}

// isListenable returns true if the address is not a link-local unicast address
// and is served locally.
func isListenable(addr netip.Addr) (ok bool) {
	return addr.IsPrivate() || addr.IsLoopback()
}

// allListenableAddresses returns all the addresses of network interfaces that
// are local and are not link-local unicast addresses.
func allListenableAddresses() (laddrs []*ipPortConfig, err error) {
	netAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("getting interfaces addresses: %w", err)
	}

	set := netutil.SubnetSetFunc(isListenable)

	return filterInterfaceAddrs(netAddrs, set, defaultPlainDNSPort), nil
}

// newDefaultServerConfig creates a new server configuration with the local
// addresses of the machine.
func newDefaultServerConfig() (c *serverConfig, err error) {
	defer func() { err = errors.Annotate(err, "creating default server configuration: %w") }()

	localAddrs, err := allListenableAddresses()
	if err != nil {
		// Don't wrap the error since there is already an annotation deferred.
		return nil, err
	}

	return &serverConfig{
		BindRetry: &bindRetryConfig{
			Enabled:  defaultBindRetryEnabled,
			Interval: timeutil.Duration(defaultBindRetryIvl),
			Count:    defaultBindRetryCount,
		},
		ListenAddresses: localAddrs,
		PendingRequests: &pendingRequestsConfig{
			Enabled: defaultPendingRequestsEnabled,
		},
	}, nil
}

// newDefaultDNSConfig creates a new default configuration for DNS.
func newDefaultDNSConfig() (c *dnsConfig, err error) {
	defer func() { err = errors.Annotate(err, "creating default dns configuration: %w") }()

	serverConf, err := newDefaultServerConfig()
	if err != nil {
		// Don't wrap the error since there is already an annotation deferred.
		return nil, err
	}

	bootstrapServers := []*ipPortConfig{{
		Address: netip.AddrPortFrom(netip.MustParseAddr("9.9.9.10"), defaultPlainDNSPort),
	}, {
		Address: netip.AddrPortFrom(netip.MustParseAddr("149.112.112.10"), defaultPlainDNSPort),
	}, {
		Address: netip.AddrPortFrom(netip.MustParseAddr("2620:fe::10"), defaultPlainDNSPort),
	}, {
		Address: netip.AddrPortFrom(netip.MustParseAddr("2620:fe::fe:10"), defaultPlainDNSPort),
	}}
	upstreamGroups := upstreamGroupsConfig{
		agdc.UpstreamGroupNameDefault: &upstreamGroupConfig{
			Address: defaultUpstreamAddress,
			// TODO(e.burkov):  It marshals into an empty slice, but should not
			// appear in the configuration file at all.
			Match: nil,
			Autodevice: &autodeviceConfig{
				Enabled: false,
			},
		},
	}
	fallbackServers := []*urlConfig{{
		Address: defaultFallbackAddress,
	}}

	return &dnsConfig{
		Server: serverConf,
		Cache: &cacheConfig{
			Enabled:    defaultCacheEnabled,
			Size:       defaultCacheSize,
			ClientSize: defaultCacheClientSize,
		},
		Bootstrap: &bootstrapConfig{
			Servers: bootstrapServers,
			Timeout: timeutil.Duration(defaultUpstreamTimeout),
		},
		Upstream: &upstreamConfig{
			Groups:  upstreamGroups,
			Timeout: timeutil.Duration(defaultUpstreamTimeout),
		},
		Fallback: &fallbackConfig{
			Servers: fallbackServers,
			Timeout: timeutil.Duration(defaultUpstreamTimeout),
		},
	}, nil
}

// newDefaultConfig creates a new ready-to-use default configuration for a newly
// installed service.
func newDefaultConfig() (c *configuration, err error) {
	defer func() { err = errors.Annotate(err, "creating default configuration: %w") }()

	dnsConf, err := newDefaultDNSConfig()
	if err != nil {
		// Don't wrap the error since there is already an annotation deferred.
		return nil, err
	}

	return &configuration{
		DNS: dnsConf,
		Debug: &debugConfig{
			Pprof: &pprofConfig{
				Enabled: defaultPprofEnabled,
				Port:    defaultPprofPort,
			},
		},
		Log: &logConfig{
			Output:    defaultLogOutput,
			Format:    defaultLogFormat,
			Timestamp: defaultLogTimestamp,
			Verbose:   defaultLogVerbose,
		},
		SchemaVersion: configmigrate.VersionLatest,
	}, nil
}

// writeDefaultConfig writes the default configuration to the file at path.  If
// the file at path already exists, it does nothing.
func writeDefaultConfig(path string) (err error) {
	defer func() { err = errors.Annotate(err, "writing default configuration: %w") }()

	// #nosec G304 -- Trust the path to the configuration file that is currently
	// expected to be in the same directory as the binary.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			// TODO(e.burkov):  Log properly.
			_, _ = fmt.Fprintf(os.Stderr, "using configuration file %q\n", path)

			return nil
		}

		return fmt.Errorf("creating configuration file: %w", err)
	}
	defer func() { err = errors.WithDeferred(err, f.Close()) }()

	_, _ = fmt.Fprintln(os.Stderr, "creating default configuration")

	conf, err := newDefaultConfig()
	if err != nil {
		// Don't wrap the error since it's informative enough as is.
		return err
	}

	_, _ = fmt.Fprintln(os.Stderr, "writing default configuration")

	return yaml.NewEncoder(f).Encode(conf)
}
