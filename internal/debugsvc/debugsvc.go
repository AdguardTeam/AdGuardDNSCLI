// Package debugsvc contains the debug HTTP API.
package debugsvc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"

	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/netutil/httputil"
	"github.com/AdguardTeam/golibs/service"
	"github.com/AdguardTeam/golibs/version"
)

// Config is the configuration for the debug HTTP API service.  See [New].  All
// fields must not be empty.
type Config struct {
	// Logger is used as the base logger for the debug API service.
	Logger *slog.Logger

	// InitialAddr is the address on which to serve the debug API.
	InitialAddr netip.AddrPort
}

// Service is a [service.Interface] that handles the debug HTTP API.
type Service struct {
	logger    *slog.Logger
	srv       *httputil.Server
	userAgent string
}

// New returns a properly initialized *Service.  c must not be nil.
func New(c *Config) (svc *Service) {
	svc = &Service{
		// TODO(f.setrakov): Fix loggers.
		logger:    c.Logger,
		userAgent: "AdGuardDNSCLI/" + version.Version(),
	}

	mux := http.NewServeMux()
	svc.route(mux)

	srvHdrMw := httputil.ServerHeaderMiddleware(svc.userAgent)
	reqIDMw := httputil.NewRequestIDMiddleware()
	handler := httputil.Wrap(mux, srvHdrMw, reqIDMw)

	// #nosec G112 -- Do not set the timeouts, since debug/pprof and similar
	// debug APIs may be busy for a long time.
	svc.srv = httputil.NewServer(&httputil.ServerConfig{
		InitialAddress: c.InitialAddr,
		BaseLogger:     c.Logger,
		Server: &http.Server{
			Addr:    c.InitialAddr.String(),
			Handler: handler,
		},
	})

	return svc
}

// LocalAddr returns the local address of the server.  It must not be called
// before or at the same time as [Service.Start] and after or at the same time
// as [Service.Shutdown].
func (svc *Service) LocalAddr() (addr net.Addr) {
	return svc.srv.LocalAddr()
}

// type check
var _ service.Interface = (*Service)(nil)

// Start implements the [service.Interface] interface for *Service.  Start does
// not block.  If the server fails to start, it causes an unhandled panic.
func (svc *Service) Start(ctx context.Context) (err error) {
	svc.logger.InfoContext(ctx, "starting")
	defer svc.logger.InfoContext(ctx, "started")

	go func() {
		err = svc.srv.Start(ctx)
		if err != nil {
			panic(fmt.Errorf("debugsvc: %w", err))
		}
	}()

	return nil
}

// Shutdown implements the [service.Interface] interface for *Service.
func (svc *Service) Shutdown(ctx context.Context) (err error) {
	svc.logger.InfoContext(ctx, "shutting down")
	defer svc.logger.InfoContext(ctx, "shut down")

	return errors.Annotate(svc.srv.Shutdown(ctx), "debugsvc: shutting down server: %w")
}
