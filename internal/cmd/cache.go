package cmd

import (
	"math"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/dnssvc"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/validate"
	"github.com/c2h5oh/datasize"
)

// cacheConfig is the configuration for the DNS results cache.
type cacheConfig struct {
	// Enabled specifies if the cache should be used.
	Enabled bool `yaml:"enabled"`

	// Size is the maximum size of the cache.
	Size datasize.ByteSize `yaml:"size"`

	// ClientSize is the maximum size of the cache per client.
	ClientSize datasize.ByteSize `yaml:"client_size"`
}

// toInternal converts the cache configuration to the internal representation.
// c must be valid.
func (c *cacheConfig) toInternal() (conf *dnssvc.CacheConfig) {
	return &dnssvc.CacheConfig{
		Enabled: c.Enabled,
		Size:    c.Size,
	}
}

// type check
var _ validate.Interface = (*cacheConfig)(nil)

// Validate implements the [validate.Interface] interface for *cacheConfig.
func (c *cacheConfig) Validate() (err error) {
	if c == nil {
		return errors.ErrNoValue
	}

	if !c.Enabled {
		// Don't validate cache settings if it's disabled.
		return nil
	}

	return errors.Join(
		// TODO(e.burkov):  Remove [math.MaxInt] constraint when
		// [datasize.ByteSize] is supported by proxy.
		validate.InRange("size", c.Size, 1, math.MaxInt),
		validate.InRange("client_size", c.ClientSize, 1, math.MaxInt),
	)
}
