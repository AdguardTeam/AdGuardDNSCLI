package client

import (
	"fmt"
	"maps"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/miekg/dns"
)

// Constants for valid encrypted DNS upstream schemes.
const (
	// SchemeHTTPS is the scheme for DNS-over-HTTPS upstreams.
	SchemeHTTPS = "https"

	// SchemeQUIC is the scheme for DNS-over-QUIC upstreams.
	SchemeQUIC = "quic"

	// SchemeTLS is the scheme for DNS-over-TLS upstreams.
	SchemeTLS = "tls"
)

// AutodeviceUpstreamConfig defines the configuration for clients that are
// automatically created on demand.
type AutodeviceUpstreamConfig struct {
	// UpstreamTemplate is a template for creating upstream configurations for
	// new clients.  It must be valid and have an encrypted DNS protocol scheme,
	// i.e.:
	//  - [SchemeHTTPS]
	//  - [SchemeQUIC]
	//  - [SchemeTLS]
	UpstreamTemplate *url.URL

	// Options are used to create dynamic upstreams.
	Options *upstream.Options

	// DeviceType specifies the type of device that will be created for new
	// clients.  It must be valid.
	DeviceType DeviceType

	// ProfileID specifies the profile to which new clients will be added.  It
	// must be valid.
	ProfileID ProfileID
}

// address returns an upstream address for a client with id.  id must be valid.
func (c *AutodeviceUpstreamConfig) address(id HumanID) (addr string, err error) {
	_, err = newHumanID(string(id))
	if err != nil {
		return "", fmt.Errorf("human id: %w", err)
	}

	extIDParts := []string{string(c.DeviceType), string(c.ProfileID), string(id)}
	extID := strings.Join(extIDParts, "-")

	tmpl := netutil.CloneURL(c.UpstreamTemplate)

	switch strings.ToLower(tmpl.Scheme) {
	case SchemeHTTPS:
		return tmpl.JoinPath(extID).String(), nil
	case SchemeQUIC, SchemeTLS:
		tmpl.Host = extID + "." + tmpl.Host

		return tmpl.String(), nil
	default:
		return "", fmt.Errorf("upstream template: %w: %q", errors.ErrBadEnumValue, tmpl.Scheme)
	}
}

// AutodeviceClientConfig is the mapping of question domains to autodevice
// client configurations.  Its keys, if not empty, must be valid non-FQDN domain
// names.  Its values must be valid.
type AutodeviceClientConfig map[string]*AutodeviceUpstreamConfig

// toInternal converts the AutodeviceClientConfig into a proxy.UpstreamConfig.
// hid must be valid.
func (conf AutodeviceClientConfig) toInternal(hid HumanID) (uc *proxy.UpstreamConfig, err error) {
	upsConf := &proxy.UpstreamConfig{}
	defer func() {
		if err != nil {
			err = errors.WithDeferred(err, upsConf.Close())
		}
	}()

	known := map[string]upstream.Upstream{}

	for _, domain := range slices.Sorted(maps.Keys(conf)) {
		domainConf := conf[domain]

		var addr string
		addr, err = domainConf.address(hid)
		if err != nil {
			return nil, fmt.Errorf("building upstream address for domain %q: %w", domain, err)
		}

		var u upstream.Upstream
		u, err = newUpstreamOrCached(addr, known, domainConf.Options)
		if err != nil {
			return nil, fmt.Errorf("creating upstream for domain %q: %w", domain, err)
		}

		addUpstream(upsConf, domain, u)
	}

	return upsConf, nil
}

// autodeviceClient is a dynamic client configuration matched by subnet.
type autodeviceClient struct {
	conf *proxy.CustomUpstreamConfig
}

// newAutodeviceClient creates a new autodevice client with hid and c.  hid must
// be valid, c must not be nil.
func newAutodeviceClient(hid HumanID, c *autodeviceConfig) (cli *autodeviceClient, err error) {
	upsConf, err := c.conf.toInternal(hid)
	if err != nil {
		return nil, fmt.Errorf("creating upstream configuration for autodevice client: %w", err)
	}

	return &autodeviceClient{
		conf: proxy.NewCustomUpstreamConfig(upsConf, c.cacheEnabled, c.cacheSize, false),
	}, nil
}

// type check
var _ Client = (*autodeviceClient)(nil)

// Upstreams implements the [Client] interface for *autodeviceClient.
func (c *autodeviceClient) Upstreams() (uc *proxy.CustomUpstreamConfig) {
	return c.conf
}

// newUpstreamOrCached creates a new upstream or returns the cached one from
// addrToUps.
//
// TODO(e.burkov):  DRY with version from [cmd].
func newUpstreamOrCached(
	addr string,
	addrToUps map[string]upstream.Upstream,
	opts *upstream.Options,
) (u upstream.Upstream, err error) {
	u, ok := addrToUps[addr]
	if !ok {
		u, err = upstream.AddressToUpstream(addr, opts)
		if err != nil {
			// Don't wrap the error, because it's informative enough as is.
			return nil, err
		}

		addrToUps[addr] = u
	}

	return u, nil
}

// addUpstream adds an upstream to conf for the given domain.  If domain is
// empty, the upstream is added to the general list of upstreams.  conf must not
// be nil, u must be valid.  domain, if not empty, must be a valid non-FQDN
// domain name.
func addUpstream(conf *proxy.UpstreamConfig, domain string, u upstream.Upstream) {
	if domain == "" {
		conf.Upstreams = append(conf.Upstreams, u)

		return
	}

	if conf.DomainReservedUpstreams == nil {
		conf.DomainReservedUpstreams = map[string][]upstream.Upstream{}
	}
	if conf.SpecifiedDomainUpstreams == nil {
		conf.SpecifiedDomainUpstreams = map[string][]upstream.Upstream{}
	}

	domain = dns.Fqdn(strings.ToLower(domain))
	conf.DomainReservedUpstreams[domain] = append(conf.DomainReservedUpstreams[domain], u)
	conf.SpecifiedDomainUpstreams[domain] = append(conf.SpecifiedDomainUpstreams[domain], u)
}

// autodeviceConfig is a complete configuration for an autodevice client under
// specific IP subnet.
type autodeviceConfig struct {
	conf         AutodeviceClientConfig
	prefix       netip.Prefix
	cacheSize    int
	cacheEnabled bool
}

// compare is a method for sorting autodevice configurations by prefix.  Empty
// prefix is sorted last, so that it is only used if no other configuration
// matches.  other must not be nil.
func (c *autodeviceConfig) compare(other *autodeviceConfig) (res int) {
	switch {
	case c.prefix == (netip.Prefix{}):
		if other.prefix == (netip.Prefix{}) {
			return 0
		}

		return 1
	case other.prefix == (netip.Prefix{}):
		return -1
	default:
		return c.prefix.Compare(other.prefix)
	}
}
