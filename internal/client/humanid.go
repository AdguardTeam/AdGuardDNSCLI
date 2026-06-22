package client

import (
	"context"
	"net/netip"
	"strings"
	"time"

	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/timeutil"
)

// HumanID is an identifier for DNS client.  It must be unique for each client
// among a single [Storage].
type HumanID string

// ValidHumanID is a HumanID that is valid until a certain time.
type ValidHumanID struct {
	// Until is the time until which ID is valid.  It must not be empty.
	Until time.Time

	// ID is the identifier for a client.  It must be valid for at least until
	// Until.
	ID HumanID
}

// HumanIDSource is an interface for retrieving clients' identifiers by their
// address.
type HumanIDSource interface {
	// Identify returns an identification info for a client with the given
	// address.  If there is no error, id must not be nil.  addr must be valid.
	Identify(ctx context.Context, addr netip.Addr) (id *ValidHumanID, err error)
}

// EmptyHumanIDSource is an empty [HumanIDSource].
type EmptyHumanIDSource struct{}

// type check
var _ HumanIDSource = EmptyHumanIDSource{}

// Identify implements the [HumanIDSource] interface for [EmptyHumanIDSource].
// It always returns a nil id and [errors.ErrNoValue].
func (EmptyHumanIDSource) Identify(_ context.Context, _ netip.Addr) (id *ValidHumanID, err error) {
	return nil, errors.ErrNoValue
}

// DefaultHumanIDSourceConfig is the configuration for [DefaultHumanIDSource].
type DefaultHumanIDSourceConfig struct {
	// Clock is used for determining the validity of IDs.  It must not be nil.
	Clock timeutil.Clock

	// ValidityIvl is a time interval of validity.
	ValidityIvl timeutil.Duration
}

// DefaultHumanIDSource is a simple [HumanIDSource] that generates a HumanID
// based solely on the given address.
//
// TODO(m.kazantsev):  Use.
type DefaultHumanIDSource struct {
	clock       timeutil.Clock
	validityIvl time.Duration
}

// NewDefaultHumanIDSource returns properly initialized *DefaultHumanIDSource.
// conf must be non-nil and valid.
func NewDefaultHumanIDSource(conf *DefaultHumanIDSourceConfig) (hs *DefaultHumanIDSource) {
	return &DefaultHumanIDSource{
		clock:       conf.Clock,
		validityIvl: time.Duration(conf.ValidityIvl),
	}
}

// type check
var _ HumanIDSource = (*DefaultHumanIDSource)(nil)

// Identify implements the [HumanIDSource] interface for *DefaultHumanIDSource.
func (d *DefaultHumanIDSource) Identify(
	ctx context.Context,
	addr netip.Addr,
) (id *ValidHumanID, err error) {
	addr = addr.Unmap()
	hostname := addr.StringExpanded()

	if addr.Is4() {
		hostname = strings.ReplaceAll(hostname, ".", "-")
	} else {
		hostname = strings.ReplaceAll(hostname, ":", "-")
	}

	id = &ValidHumanID{
		ID:    HumanID("dev-" + hostname),
		Until: d.clock.Now().Add(d.validityIvl),
	}

	return id, nil
}

// ConsequentHumanIDSource concatenates multiple HumanIDSource instances.
//
// TODO(m.kazantsev):  Consider removing this implementation
type ConsequentHumanIDSource []HumanIDSource

// type check
var _ HumanIDSource = ConsequentHumanIDSource{}

// Identify implements the [HumanIDSource] interface for ConsequentIDSource.  If
// one or more errors are returned from sources of the consequence, they are
// ignored.
func (c ConsequentHumanIDSource) Identify(
	ctx context.Context,
	addr netip.Addr,
) (id *ValidHumanID, err error) {
	for _, src := range c {
		id, err = src.Identify(ctx, addr)
		if err != nil {
			continue
		}

		return id, nil
	}

	return nil, errors.ErrNoValue
}
