package cmd

import (
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/agdc"
	"github.com/AdguardTeam/AdGuardDNSCLI/internal/agdcslog"
	"github.com/AdguardTeam/AdGuardDNSCLI/internal/client"
	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/AdguardTeam/golibs/timeutil"
	"github.com/AdguardTeam/golibs/validate"
	"github.com/miekg/dns"
)

// upstreamConfig is the configuration for the DNS upstream servers.
type upstreamConfig struct {
	// Groups contains all the groups of servers.
	Groups upstreamGroupsConfig `yaml:"groups"`

	// Timeout constrains the time for sending requests and receiving responses.
	Timeout timeutil.Duration `yaml:"timeout"`
}

// type check
var _ validate.Interface = (*upstreamConfig)(nil)

// Validate implements the [validate.Interface] interface for *upstreamConfig.
func (c *upstreamConfig) Validate() (err error) {
	if c == nil {
		return errors.ErrNoValue
	}

	errs := []error{
		validate.Positive("timeout", c.Timeout),
	}
	errs = validate.Append(errs, "groups", c.Groups)

	return errors.Join(errs...)
}

// indexedMatch is a key for matchSet.  It's essentially an
// [upstreamMatchConfig] with a lowercased question domain.
type indexedMatch struct {
	domain string
	client netip.Prefix
}

// matchSet validates that no two matches have the same domain and client in
// different upstream groups.
type matchSet map[indexedMatch]agdc.UpstreamGroupName

// addMatch returns an error if m conflicts with the ones in s.  name is the
// name of the group containing m.
//
// TODO(e.burkov):  Validate the prefixes don't overlap if not equal.
func (s matchSet) addMatch(name agdc.UpstreamGroupName, m *upstreamMatchConfig) (err error) {
	key := m.toIndexedMatch()
	another, ok := s[key]
	if !ok {
		s[key] = name

		return nil
	}

	if another == name {
		return errors.ErrDuplicated
	}

	return fmt.Errorf("conflicts with group %q", another)
}

// upstreamGroupsConfig is the configuration for a set of groups of DNS upstream
// servers.
type upstreamGroupsConfig map[agdc.UpstreamGroupName]*upstreamGroupConfig

// requiredGroups is the list of groups that must be present in a valid
// [upstreamGroupsConfig].
var requiredGroups = []agdc.UpstreamGroupName{
	agdc.UpstreamGroupNameDefault,
}

// predefinedGroups is the list of groups that must have no match criteria in a
// valid [upstreamGroupsConfig].
var predefinedGroups = []agdc.UpstreamGroupName{
	agdc.UpstreamGroupNameDefault,
	agdc.UpstreamGroupNamePrivate,
}

// type check
var _ validate.Interface = (upstreamGroupsConfig)(nil)

// Validate implements the [validate.Interface] interface for
// upstreamGroupsConfig.
func (c upstreamGroupsConfig) Validate() (err error) {
	if c == nil {
		return errors.ErrNoValue
	}

	var errs []error
	for _, name := range requiredGroups {
		if _, ok := c[name]; !ok {
			err = fmt.Errorf("group %q: must be present", name)
			errs = append(errs, err)
		}
	}

	errs = c.validateGroups(errs)

	return errors.Join(errs...)
}

// validateGroups appends the errors of validating groups within c to errs and
// returns the result.
func (c upstreamGroupsConfig) validateGroups(errs []error) (res []error) {
	ms := matchSet{}
	for _, name := range slices.Sorted(maps.Keys(c)) {
		g := c[name]

		var err error
		if slices.Contains(predefinedGroups, name) {
			err = g.validateAsPredefined()
		} else {
			err = g.validateAsCustom(ms, name)
		}
		if err != nil {
			err = fmt.Errorf("group %q: %w", name, err)
			errs = append(errs, err)
		}
	}

	return errs
}

// upstreamGroupConfig is the configuration for a group of DNS upstream servers.
type upstreamGroupConfig struct {
	// Address is the URL of the upstream server for this group.
	Address string `yaml:"address"`

	// Autodevice is the configuration for creating upstreams automatically for
	// this group.
	Autodevice *autodeviceConfig `yaml:"autodevice"`

	// Match is the set of criteria for choosing this group.
	Match []*upstreamMatchConfig `yaml:"match"`
}

// validateAsPredefined returns an error if c is not a valid predefined group
// configuration that should have no match criteria.
func (c *upstreamGroupConfig) validateAsPredefined() (err error) {
	if c == nil {
		return errors.ErrNoValue
	}

	// TODO(e.burkov):  !! private group doesn't and can't support autodevice.

	return errors.Join(
		validate.NotEmpty("address", c.Address),
		errors.Annotate(c.Autodevice.Validate(), "autodevice: %w"),
		validate.EmptySlice("match", c.Match),
	)
}

// validateAsCustom returns an error if c is not a valid custom group
// configuration for group named n within the set s.
func (c *upstreamGroupConfig) validateAsCustom(s matchSet, n agdc.UpstreamGroupName) (err error) {
	if c == nil {
		return errors.ErrNoValue
	}

	errs := []error{
		validate.NotEmpty("address", c.Address),
		errors.Annotate(c.Autodevice.Validate(), "autodevice: %w"),
	}

	for i, m := range c.Match {
		err = m.validate(s, n)
		if err != nil {
			err = fmt.Errorf("match: at index %d: %w", i, err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// upstreamMatchConfig is the configuration for criteria for choosing an
// upstream group.
type upstreamMatchConfig struct {
	// Client is the client's subnet to match.  Prefix itself should be masked.
	Client netutil.Prefix `yaml:"client"`

	// QuestionDomain is the domain name from request's question to match.
	QuestionDomain string `yaml:"question_domain"`
}

// validate returns error if c is not valid.
func (c *upstreamMatchConfig) validate(s matchSet, name agdc.UpstreamGroupName) (err error) {
	switch {
	case c == nil:
		return errors.ErrNoValue
	case *c == (upstreamMatchConfig{}):
		return errors.ErrEmptyValue
	default:
		return c.validateValues(s, name)
	}
}

// validateValues returns error if c contains invalid values.  c must not be
// nil.
func (c *upstreamMatchConfig) validateValues(s matchSet, name agdc.UpstreamGroupName) (err error) {
	var errs []error

	if c.QuestionDomain != "" {
		err = netutil.ValidateDomainName(c.QuestionDomain)
		if err != nil {
			err = fmt.Errorf("question_domain: %w", err)
			errs = append(errs, err)
		}
	}

	// TODO(e.burkov):  It may be useful to be able to specify the whole address
	// and only change the mask.
	if c.Client.Prefix != c.Client.Masked() {
		bitNum := c.Client.Bits()
		err = fmt.Errorf("client: %s must has at most %d significant bits", c.Client, bitNum)
		errs = append(errs, err)
	}

	errs = append(errs, s.addMatch(name, c))

	return errors.Join(errs...)
}

// toIndexedMatch converts the upstream match configuration to a key for
// [matchSet].
func (c *upstreamMatchConfig) toIndexedMatch() (im indexedMatch) {
	return indexedMatch{
		domain: strings.ToLower(c.QuestionDomain),
		client: c.Client.Prefix,
	}
}

func classifyUpstreams(
	conf *upstreamConfig,
	baseLogger *slog.Logger,
	boot upstream.Resolver,
	general *proxy.UpstreamConfig,
	private *proxy.UpstreamConfig,
	static map[netip.Prefix]*proxy.UpstreamConfig,
	autodevice map[netip.Prefix]client.AutodeviceClientConfig,
) (err error) {
	defer func() { err = errors.Annotate(err, "creating upstreams: %w") }()

	known := map[string]upstream.Upstream{}

	var errs []error
	for name, g := range conf.Groups {
		opts := &upstream.Options{
			Logger: baseLogger.With(
				agdcslog.KeyUpstreamType, agdcslog.UpstreamTypeMain,
				agdcslog.KeyUpstreamGroup, name,
			),
			Timeout:   time.Duration(conf.Timeout),
			Bootstrap: boot,
		}

		if g.Autodevice.Enabled {
			err = g.addAutodeviceGroup(autodevice, opts)
			errs = append(errs, errors.Annotate(err, "group %q: %w", name))
		} else {
			err = g.classifyCommonGroup(name, general, private, static, opts, known)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (c *upstreamGroupConfig) classifyCommonGroup(
	name agdc.UpstreamGroupName,
	general *proxy.UpstreamConfig,
	private *proxy.UpstreamConfig,
	static map[netip.Prefix]*proxy.UpstreamConfig,
	opts *upstream.Options,
	known map[string]upstream.Upstream,
) (err error) {
	var u upstream.Upstream
	u, err = newUpstreamOrCached(c.Address, known, opts)
	if err != nil {
		// Don't wrap the error, since it's informative enough as is.
		return err
	}

	switch name {
	case agdc.UpstreamGroupNameDefault:
		general.Upstreams = append(general.Upstreams, u)
	case agdc.UpstreamGroupNamePrivate:
		private.Upstreams = append(private.Upstreams, u)
	default:
		c.addCommonGroup(general, static, u)
	}
	return nil
}

// newUpstreamOrCached creates a new upstream or returns the cached one from
// addrToUps.
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

func (c *upstreamGroupConfig) addAutodeviceGroup(
	autodevice map[netip.Prefix]client.AutodeviceClientConfig,
	opts *upstream.Options,
) (err error) {
	for _, m := range c.Match {
		cliConf := autodevice[netip.Prefix{}]

		pref := m.Client.Prefix
		if pref != (netip.Prefix{}) {
			cliConf = autodevice[pref]
			if cliConf == nil {
				cliConf = client.AutodeviceClientConfig{}
				autodevice[pref] = cliConf
			}
		}

		_, ok := cliConf[m.QuestionDomain]
		if ok {
			return fmt.Errorf(
				"group for client %q and domain %q: %w",
				pref,
				m.QuestionDomain,
				errors.ErrDuplicated,
			)
		}

		upsAddr, err := url.Parse(c.Address)
		if err != nil {
			return fmt.Errorf(
				"group for client %q and domain %q: address: %w",
				pref,
				m.QuestionDomain,
				err,
			)
		}

		cliConf[m.QuestionDomain] = &client.AutodeviceUpstreamConfig{
			UpstreamTemplate: upsAddr,
			DeviceType:       client.DeviceType(c.Autodevice.DeviceType),
			ProfileID:        client.ProfileID(c.Autodevice.ProfileID),
			Options:          opts,
		}
	}

	return nil
}

func (c *upstreamGroupConfig) addCommonGroup(
	general *proxy.UpstreamConfig,
	static map[netip.Prefix]*proxy.UpstreamConfig,
	u upstream.Upstream,
) {
	for _, m := range c.Match {
		conf := general

		pref := m.Client.Prefix
		if pref != (netip.Prefix{}) {
			conf = static[pref]
			if conf == nil {
				conf = &proxy.UpstreamConfig{}
				static[pref] = conf
			}
		}

		domain := m.QuestionDomain
		if domain == "" {
			conf.Upstreams = append(conf.Upstreams, u)

			continue
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
}
