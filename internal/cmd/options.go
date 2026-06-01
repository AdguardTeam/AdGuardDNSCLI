package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/version"
)

// options specifies the command-line options.
//
// TODO(e.burkov):  Add an option to prevalidate configuration file.
type options struct {
	// serviceAction specifies the action to perform with the service.  See
	// [serviceAction] for the list of possible values.
	serviceAction serviceAction

	// verbose specifies whether to enable verbose output.
	verbose bool

	// version makes the application print the version to stdout and exit with
	// a success status-code.
	version bool

	// help makes the application print the usage information to stdout and exit
	// with a success status-code.
	help bool
}

// parseOptions parses the command-line options.
//
// TODO(e.burkov):  Use [flag.NewFlagSet].
func parseOptions() (opts *options, err error) {
	const (
		optionService      = "s"
		descriptionService = "service action to perform, one of: " +
			string(serviceActionInstall) + ", " +
			string(serviceActionRestart) + ", " +
			string(serviceActionStart) + ", " +
			string(serviceActionStatus) + ", " +
			string(serviceActionStop) + ", " +
			string(serviceActionUninstall)

		optionVerbose      = "v"
		descriptionVerbose = "enable verbose logging"

		optionVersion      = "version"
		descriptionVersion = "print version to stdout and exit"

		optionHelp      = "h"
		descriptionHelp = "print this help to stdout and exit"
	)

	flag.CommandLine.Init(os.Args[0], flag.ContinueOnError)
	opts = &options{}

	flag.Var(&opts.serviceAction, optionService, descriptionService)
	flag.BoolVar(&opts.verbose, optionVerbose, false, descriptionVerbose)
	flag.BoolVar(&opts.version, optionVersion, false, descriptionVersion)
	flag.BoolVar(&opts.help, optionHelp, false, descriptionHelp)

	var errs []error

	flag.CommandLine.SetOutput(io.Discard)

	err = flag.CommandLine.Parse(os.Args[1:])
	errs = append(errs, err)

	if len(flag.Args()) > 0 {
		err = fmt.Errorf("unexpected arguments: %q", flag.Args())
		errs = append(errs, err)
	}

	return opts, errors.Join(errs...)
}

// handleInfoOpts returns true if the options contained -h or --version flags.
// It also prints the corresponding messages to stdout.
func (opts *options) handleInfoOpts() (needsExit bool) {
	if opts.help {
		flag.CommandLine.SetOutput(os.Stdout)
		flag.CommandLine.Usage()
	}

	if opts.version {
		_, _ = fmt.Fprintln(os.Stdout, version.Version())
	}

	return opts.help || opts.version
}
