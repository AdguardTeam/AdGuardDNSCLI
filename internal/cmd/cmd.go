// Package cmd is the AdGuard DNS CLI entry point.
package cmd

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/osutil"
	osservice "github.com/kardianos/service"
)

// Main is the entrypoint of AdGuard DNS CLI.  Main may accept arguments, such
// as embedded assets and command-line arguments.
func Main() {
	// TODO(a.garipov):  Use for start cancelation.
	ctx := context.Background()

	opts, err := parseOptions()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		flag.CommandLine.SetOutput(os.Stderr)
		flag.CommandLine.Usage()

		os.Exit(osutil.ExitCodeArgumentError)
	}

	if opts.handleInfoOpts() {
		os.Exit(osutil.ExitCodeSuccess)
	}

	envs, envsErrs := parseLogEnvs()
	l, logFile, envsLoggerErr := newEnvLogger(ctx, opts, envs)

	conf, err := handleServiceConfig(ctx, l, opts.serviceAction)
	l, logFile, confLoggerErrs := newConfigLogger(ctx, l, logFile, opts, envs, conf)

	reportPrevErrs(ctx, l, envsErrs, envsLoggerErr, confLoggerErrs)

	prog := &program{
		conf:    conf,
		errCh:   make(chan error),
		logger:  l,
		logFile: logFile,
	}

	check(ctx, prog, err)

	svc, err := osservice.New(prog, newServiceConfig())
	check(ctx, prog, err)

	if opts.serviceAction != "" {
		exitCode := control(svc, opts.serviceAction)
		prog.closeLogs(ctx)

		os.Exit(exitCode)
	}

	err = svc.Run()
	check(ctx, prog, err)

	prog.closeLogs(ctx)
}

// reportPrevErrs reports errors that were collected while there was no logger.
func reportPrevErrs(
	ctx context.Context,
	l *slog.Logger,
	envsErrs []error,
	loggerErr error,
	confLoggerErrs []error,
) {
	if err := errors.Join(envsErrs...); err != nil {
		l.ErrorContext(ctx, "parsing environment", slogutil.KeyError, err)
	}

	if loggerErr != nil {
		l.ErrorContext(ctx, "creating env logger", slogutil.KeyError, loggerErr)
	}

	if err := errors.Join(confLoggerErrs...); err != nil {
		l.ErrorContext(ctx, "creating conf logger", slogutil.KeyError, err)
	}
}

// check writes the error to the program's log and exits the process with a
// failure code if the error is not nil.  It also closes log files.  It must
// only be called within [Main].
func check(ctx context.Context, prog *program, err error) {
	if err == nil {
		return
	}

	prog.logger.ErrorContext(ctx, "fatal error", slogutil.KeyError, err)
	prog.closeLogs(ctx)

	os.Exit(osutil.ExitCodeFailure)
}
