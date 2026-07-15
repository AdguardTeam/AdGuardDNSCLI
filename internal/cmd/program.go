package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/debugsvc"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/AdguardTeam/golibs/service"
	"github.com/AdguardTeam/golibs/version"
	osservice "github.com/kardianos/service"
)

// program is the implementation of the [osservice.Interface] interface for
// AdGuard DNS CLI.
type program struct {
	// TODO(e.burkov):  Add *options?

	// conf is the parsed configuration to run the program.  It appears nil on
	// any service action and must not be accessed.
	conf       *configuration
	baseLogger *slog.Logger

	// TODO(e.burkov):  Use [io.Closer].
	logFile *os.File
	done    chan struct{}
	errCh   chan error
}

// type check
var _ osservice.Interface = (*program)(nil)

// serviceProgramPrefix is the default and recommended prefix for the logger of
// the default [osservice.Interface] implementation.
const serviceProgramPrefix = "program"

// Start implements the [osservice.Interface] interface for [*program].
func (prog *program) Start(_ osservice.Service) (err error) {
	ctx := context.Background()
	l := prog.baseLogger.With(slogutil.KeyPrefix, serviceProgramPrefix)

	// TODO(a.garipov): Copy logs configuration from the WIP abt. slog.
	l.InfoContext(
		ctx,
		"AdGuard DNS CLI starting",
		"version", version.Version(),
		"revision", version.Revision(),
		"branch", version.Branch(),
		"commit_time", version.CommitTime(),
		"race", version.RaceEnabled,
		"verbose", l.Enabled(ctx, slog.LevelDebug),
	)

	svcHdlr := newServiceHandler(prog.done, service.SignalHandlerShutdownTimeout)

	if prog.conf.Debug.Pprof.Enabled {
		debugSvc := debugsvc.New(&debugsvc.Config{
			Logger: prog.baseLogger.With(slogutil.KeyPrefix, "debugsvc"),
			// TODO(m.kazantsev):  Consider making configurable.
			InitialAddr: netip.AddrPortFrom(netutil.IPv4Localhost(), prog.conf.Debug.Pprof.Port),
		})

		errors.Check(debugSvc.Start(ctx))

		svcHdlr.add(debugSvc)
	}

	err = initDNSService(ctx, prog.conf.DNS, prog.baseLogger, svcHdlr)
	if err != nil {
		// Don't wrap the error, since it's informative enough as is.
		return err
	}

	l.DebugContext(ctx, "dns service started")

	svcHdlrLog := prog.baseLogger.With(slogutil.KeyPrefix, "service_handler")

	go svcHdlr.handle(ctx, svcHdlrLog, prog.errCh)

	return nil
}

// Stop implements the [osservice.Interface] interface for [*program].
func (prog *program) Stop(_ osservice.Service) (err error) {
	close(prog.done)

	return <-prog.errCh
}

// closeLogs closes the log files and syslog handler, if there are any.
func (prog *program) closeLogs(ctx context.Context) {
	// At this point, just use stderr with defaults.
	l := slogutil.New(&slogutil.Config{
		Output: os.Stderr,
	}).With(slogutil.KeyPrefix, serviceProgramPrefix)

	if prog.logFile != nil {
		err := prog.logFile.Close()
		if err != nil {
			err = fmt.Errorf("closing log file: %w", err)
			l.ErrorContext(ctx, "stopping", slogutil.KeyError, err)
		}
	}

	h := prog.baseLogger.Handler()
	if c, ok := h.(io.Closer); ok {
		err := c.Close()
		if err != nil {
			err = fmt.Errorf("closing system logger: %w", err)
			l.ErrorContext(ctx, "stopping", slogutil.KeyError, err)
		}
	}
}
