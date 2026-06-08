package client

import (
	"context"
	"net/netip"

	"github.com/AdguardTeam/golibs/service"
)

// Storage is an interface for storing clients.
type Storage interface {
	// ByAddr returns the client for addr.  c must not be nil if ok is true.  It
	// must be safe for concurrent use.
	ByAddr(ctx context.Context, addr netip.Addr) (c Client, ok bool)

	// Shutdowner is used to release resources used by the storage.  Storage and
	// its clients must not be used after Shutdown.
	service.Shutdowner
}

// EmptyStorage is an implementation of [Storage] that does nothing.
type EmptyStorage struct {
	service.Empty
}

// type check
var _ Storage = (*EmptyStorage)(nil)

// ByAddr implements the [Storage] interface for EmptyStorage.  It always
// returns nil and false.
func (EmptyStorage) ByAddr(_ context.Context, _ netip.Addr) (c Client, ok bool) {
	return nil, false
}
