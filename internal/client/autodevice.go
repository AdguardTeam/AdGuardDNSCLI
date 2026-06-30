package client

import (
	"fmt"
	"net/netip"
	"net/url"
	"runtime"
	"strings"
	"sync"

	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/miekg/dns"
)

// Constants for valid encrypted DNS upstream schemes.
const (
	schemeHTTPS = "https"
	schemeQUIC  = "quic"
	schemeTLS   = "tls"
)

// AutodeviceUpstreamConfig defines the configuration for clients that are
// automatically created on demand.
type AutodeviceUpstreamConfig struct {
	// UpstreamTemplate is a template for creating upstream configurations for
	// new clients.  It must be valid and have an encrypted DNS protocol scheme,
	// i.e.:
	//  - https
	//  - quic
	//  - tls
	UpstreamTemplate *url.URL

	// Options are used to create dynamic upstreams.
	//
	// TODO(e.burkov):  !! Consider adding contracts.
	Options *upstream.Options

	// DeviceType specifies the type of device that will be created for new
	// clients.  It must be valid.
	DeviceType DeviceType

	// ProfileID specifies the profile to which new clients will be added.  It
	// must be valid.
	ProfileID ProfileID
}

// AutodeviceClientConfig is the mapping of question domains to autodevice
// client configurations.  Its keys, if not empty, must be valid non-FQDN domain
// names.  Its values must be valid.
type AutodeviceClientConfig map[string]*AutodeviceUpstreamConfig

// Validate returns an error if c is not valid.
//
// TODO(e.burkov):  !! perhaps, remove.
func (c *AutodeviceUpstreamConfig) Validate() (err error) {
	switch {
	case c == nil:
		return errors.ErrNoValue
	case c.UpstreamTemplate == nil:
		return errors.Error("upstream template: no value")
	}

	scheme := strings.ToLower(c.UpstreamTemplate.Scheme)
	switch scheme {
	case schemeHTTPS:
		if c.UpstreamTemplate.Host == "" {
			return errors.Error("upstream template: empty host")
		}
	case schemeQUIC, schemeTLS:
		host := c.UpstreamTemplate.Hostname()
		if host == "" {
			return errors.Error("upstream template: empty host")
		}

		if ip, ipErr := netip.ParseAddr(host); ipErr == nil && ip.IsValid() {
			return errors.Error("upstream template: ip host is not supported")
		}
	default:
		return fmt.Errorf("upstream template: unsupported scheme %q", c.UpstreamTemplate.Scheme)
	}

	return nil
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
	case schemeHTTPS:
		return tmpl.JoinPath(extID).String(), nil
	case schemeQUIC, schemeTLS:
		tmpl.Host = extID + "." + tmpl.Host

		return tmpl.String(), nil
	default:
		return "", fmt.Errorf("upstream template: %w: %q", errors.ErrBadEnumValue, tmpl.Scheme)
	}
}

// autodeviceClient is a dynamic client configuration matched by subnet.
type autodeviceClient struct {
	initOnce     func() (uc *proxy.CustomUpstreamConfig)
	conf         AutodeviceClientConfig
	humanID      HumanID
	cacheSize    int
	cacheEnabled bool
}

// newAutodeviceClient creates a new autodevice client with the given
// human-readable identifier and configuration.  hid and conf must be valid.
func newAutodeviceClient(hid HumanID, c *autodeviceConfig) (cli Client) {
	autoCli := &autodeviceClient{
		humanID:      hid,
		conf:         c.conf,
		cacheSize:    c.cacheSize,
		cacheEnabled: c.cacheEnabled,
	}

	autoCli.initOnce = sync.OnceValue(autoCli.initUpstreams)

	return autoCli
}

// type check
var _ Client = (*autodeviceClient)(nil)

// Upstreams implements the [Client] interface for *autodeviceClient.
func (c *autodeviceClient) Upstreams() (uc *proxy.CustomUpstreamConfig) {
	return c.initOnce()
}

// initUpstreams initializes the upstream configuration for the autodevice
// client.
func (c *autodeviceClient) initUpstreams() (uc *proxy.CustomUpstreamConfig) {
	upstreams := map[string]upstream.Upstream{}
	upsConf := &proxy.UpstreamConfig{}
	for domain, conf := range c.conf {
		addr, err := conf.address(c.humanID)
		if err != nil {
			panic(fmt.Errorf("building upstream address for domain %q: %w", domain, err))
		}

		u, err := newUpstreamOrCached(addr, upstreams, conf.Options)
		if err != nil {
			panic(fmt.Errorf("creating upstream for domain %q: %w", domain, err))
		}

		addUpstream(upsConf, domain, u)
	}

	runtime.AddCleanup(c, func(c *proxy.UpstreamConfig) {
		// TODO(e.burkov):  !! log
		_ = c.Close()
	}, upsConf)

	return proxy.NewCustomUpstreamConfig(upsConf, c.cacheEnabled, c.cacheSize, false)
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

// compare is a method for sorting autodevice configurations by prefix.  other
// must not be nil.
func (c *autodeviceConfig) compare(other *autodeviceConfig) (res int) {
	return c.prefix.Compare(other.prefix)
}
