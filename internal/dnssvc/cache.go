package dnssvc

import "github.com/c2h5oh/datasize"

// CacheConfig is the configuration for the DNS results cache.
type CacheConfig struct {
	// Enabled specifies if the cache should be used.
	Enabled bool

	// Size is the maximum size of the common cache.
	Size datasize.ByteSize
}

// TODO(e.burkov):  Add tests.
