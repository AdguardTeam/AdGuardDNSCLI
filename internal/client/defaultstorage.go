package client

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/service"
	"github.com/AdguardTeam/golibs/timeutil"
)

// DefaultStorageConfig is a configuration structure for [DefaultStorage].
type DefaultStorageConfig struct {
	// Logger is used for logging storage operations.  It must not be nil.
	Logger *slog.Logger

	// Static is a mapping of IP prefixes to clients that are known in advance.
	// Each key and value must be valid.  Subnets, which are represented by
	// prefixes, must not overlap.
	Static map[netip.Prefix]*StaticClient

	// Clock is used to get the current time.  It must not be nil.
	Clock timeutil.Clock
}

// DefaultStorage is a default implementation of the [Storage] interface.
//
// TODO(m.kazantsev):  Use.
type DefaultStorage struct {
	clock  timeutil.Clock
	logger *slog.Logger
	// mu protects clients.
	mu      *sync.Mutex
	clients []*storedClient
}

// NewDefaultStorage creates a new properly configured *DefaultStorage.  c must
// be valid.
func NewDefaultStorage(c *DefaultStorageConfig) (s *DefaultStorage) {
	clients := make([]*storedClient, 0, len(c.Static))

	for prefix, client := range c.Static {
		cl := &storedClient{
			prefix:     prefix,
			client:     client,
			validUntil: time.Time{},
		}

		clients = append(clients, cl)
	}

	return &DefaultStorage{
		logger:  c.Logger,
		clock:   c.Clock,
		clients: clients,
		mu:      &sync.Mutex{},
	}
}

// type check
var _ Storage = (*DefaultStorage)(nil)

// ByAddr implements the [Storage] interface for *DefaultStorage.
func (d *DefaultStorage) ByAddr(ctx context.Context, addr netip.Addr) (c Client, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, cli := range d.clients {
		if cli.prefix.Contains(addr) {
			if cli.isValid(d.clock.Now()) {
				return cli.client, true
			}

			return nil, false
		}
	}

	return nil, false
}

// type check
var _ service.Shutdowner = (*DefaultStorage)(nil)

// Shutdown implements the [service.Shutdowner] interface for *DefaultStorage.
func (d *DefaultStorage) Shutdown(ctx context.Context) (err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var errs []error

	for _, c := range d.clients {
		conf := c.client.Upstreams()
		err = conf.Close()
		if err != nil {
			err = fmt.Errorf("closing upstreams for clients from %s subnet: %w", c.prefix, err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// storedClient is a client stored in [DefaultStorage].
type storedClient struct {
	validUntil time.Time
	client     Client
	prefix     netip.Prefix
}

// isValid checks whether s is valid.
func (s *storedClient) isValid(now time.Time) (ok bool) {
	return s.validUntil.IsZero() || now.Before(s.validUntil)
}
