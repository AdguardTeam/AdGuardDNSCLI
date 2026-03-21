package cmd

import (
	"context"
	"log/slog"
	"time"

	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/osutil"
	"github.com/AdguardTeam/golibs/service"
)

// serviceHandler wraps [service.SignalHandler] to shut services down.
type serviceHandler struct {
	signalHandler *service.SignalHandler
}

// newServiceHandler returns a new properly initialized *serviceHandler that
// shuts down services.  timeout is the maximum time to wait for the services
// to shut down, avoid using 0.
func newServiceHandler(l *slog.Logger, timeout time.Duration) (h *serviceHandler) {
	svcHdlr := service.NewSignalHandler(&service.SignalHandlerConfig{
		Logger:          l,
		ShutdownTimeout: timeout,
	})

	return &serviceHandler{
		signalHandler: svcHdlr,
	}
}

// add adds services to h.
//
// It must not be called concurrently with [serviceHandler.handle].
func (h *serviceHandler) add(svcs ...service.Interface) {
	h.signalHandler.AddService(svcs...)
}

// handle blocks until a termination signal is received, after which it shuts
// down all services.  ctx is used for logging and serves as the base for the
// shutdown timeout.  errCh receives the exit status.
//
// It must not be called concurrently with [serviceHandler.add].
func (h *serviceHandler) handle(ctx context.Context, l *slog.Logger, errCh chan<- error) {
	defer slogutil.RecoverAndLog(ctx, l)

	status := h.signalHandler.Handle(ctx)
	if status == osutil.ExitCodeFailure {
		errCh <- errors.Error("shutdown failed")
	} else {
		errCh <- nil
	}
}
