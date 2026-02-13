package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/osutil"
	osservice "github.com/kardianos/service"
)

// serviceName is the name used by the service and the system logger.
const serviceName = "AdGuardDNSCLI"

// newServiceConfig creates a configuration that the OS service manager uses to
// control the service.
func newServiceConfig() (conf *osservice.Config) {
	return &osservice.Config{
		Name:        serviceName,
		DisplayName: "AdGuard DNS CLI",
		Description: "A DNS client for AdGuard DNS",
	}
}

// serviceAction is a type for service actions that only allows a predefined set
// of values.
type serviceAction string

// Available service commands.
const (
	serviceActionNone      serviceAction = ""
	serviceActionInstall   serviceAction = "install"
	serviceActionRestart   serviceAction = "restart"
	serviceActionStart     serviceAction = "start"
	serviceActionStatus    serviceAction = "status"
	serviceActionStop      serviceAction = "stop"
	serviceActionUninstall serviceAction = "uninstall"
)

// type check
var _ flag.Value = (*serviceAction)(nil)

// Set implements the [flag.Value] interface for [*serviceAction].
func (a *serviceAction) Set(value string) (err error) {
	switch sa := serviceAction(value); sa {
	case
		serviceActionInstall,
		serviceActionRestart,
		serviceActionStart,
		serviceActionStatus,
		serviceActionStop,
		serviceActionUninstall:
		*a = sa

		return nil
	default:
		// Don't wrap the error, since it's wrapped by package [flag].
		return errors.ErrBadEnumValue
	}
}

// String implements the [flag.Value] interface for [serviceAction].
func (a serviceAction) String() (s string) { return string(a) }

// control performs the specified service action.  It mirrors the service logic
// from [service.Control], but reports better errors and prints them to stderr.
//
// TODO(e.burkov):  Get output from this in MSI installer and show it.
func control(svc osservice.Service, action serviceAction) (exitCode osutil.ExitCode) {
	var err error
	switch action {
	case serviceActionInstall:
		err = svc.Install()
	case serviceActionRestart:
		err = svc.Restart()
	case serviceActionStart:
		err = svc.Start()
	case serviceActionStatus:
		err = controlStatus(svc)
	case serviceActionStop:
		err = svc.Stop()
	case serviceActionUninstall:
		err = svc.Uninstall()
	default:
		panic(fmt.Errorf("action: %w: %q", errors.ErrBadEnumValue, action))
	}

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "performing action %q: %s\n", action, err)

		return osutil.ExitCodeFailure
	}

	return osutil.ExitCodeSuccess
}

// controlStatus prints the status of the system service corresponding to svc.
func controlStatus(svc osservice.Service) (err error) {
	status, err := svc.Status()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stdout, "error")

		return fmt.Errorf("retrieving status: %w", err)
	}

	var msg string
	switch status {
	case osservice.StatusRunning:
		msg = "running"
	case osservice.StatusStopped:
		msg = "stopped"
	default:
		msg = "error"

		// Don't expect [osservice.StatusUnknown] here, since it's only returned
		// on error.
		//
		// TODO(e.burkov):  Consider panicking here.
		err = fmt.Errorf("unexpected status %d", status)
	}

	_, _ = fmt.Fprintln(os.Stdout, msg)

	return err
}
