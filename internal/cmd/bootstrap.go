package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/agdcslog"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/service"
	"github.com/AdguardTeam/golibs/timeutil"
	"github.com/AdguardTeam/golibs/validate"
)

// bootstrapConfig is the configuration for resolving upstream's hostnames.
type bootstrapConfig struct {
	// Servers is the list of DNS servers to use for resolving upstream's
	// hostnames.
	Servers []*ipPortConfig `yaml:"servers"`

	// Timeout constrains the time for sending requests and receiving responses.
	Timeout timeutil.Duration `yaml:"timeout"`
}

// type check
var _ validate.Interface = (*bootstrapConfig)(nil)

// Validate implements the [validate.Interface] interface for *bootstrapConfig.
func (c *bootstrapConfig) Validate() (err error) {
	if c == nil {
		return errors.ErrNoValue
	}

	errs := []error{
		validate.Positive("timeout", c.Timeout),
	}
	errs = validate.AppendSlice(errs, "servers", c.Servers)

	return errors.Join(errs...)
}

// newResolvers creates a new bootstrap resolver and a list of upstreams to
// close on shutdown.  conf and baseLogger must not be nil.
func newResolvers(
	conf *bootstrapConfig,
	baseLogger *slog.Logger,
) (boot upstream.Resolver, cls closersShutdowner, err error) {
	defer func() { err = errors.Annotate(err, "creating bootstraps: %w") }()

	opts := &upstream.Options{
		Logger:  baseLogger.With(agdcslog.KeyUpstreamType, agdcslog.UpstreamTypeBootstrap),
		Timeout: time.Duration(conf.Timeout),
	}

	resolvers := make(upstream.ConsequentResolver, 0, len(conf.Servers))
	cls = make(closersShutdowner, 0, len(conf.Servers))

	var errs []error
	for i, ipPort := range conf.Servers {
		var b *upstream.UpstreamResolver
		b, err = upstream.NewUpstreamResolver(ipPort.Address.String(), opts)
		if err != nil {
			err = fmt.Errorf("resolvers: at index %d: %w", i, err)
			errs = append(errs, err)

			continue
		}

		resolvers = append(resolvers, upstream.NewCachingResolver(b))
		cls = append(cls, b.Upstream)
	}

	return resolvers, cls, errors.Join(errs...)
}

// closersShutdowner is the implementation of [service.Shutdowner] that
// concatenates multiple instances of io.Closer.
type closersShutdowner []io.Closer

// type check
var _ service.Shutdowner = closersShutdowner{}

// Shutdown implements the [service.Shutdowner] interface for closers.
func (cl closersShutdowner) Shutdown(ctx context.Context) (err error) {
	var errs []error

	for i, closer := range cl {
		err = closer.Close()
		if err != nil {
			err = fmt.Errorf("closing bootstrap at index %d: %w", i, err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
