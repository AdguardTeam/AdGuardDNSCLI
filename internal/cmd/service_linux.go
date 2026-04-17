//go:build linux

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/AdguardTeam/golibs/osutil/executil"
	"github.com/kardianos/service"
)

// chooseSystem checks the current system detected and substitutes it with local
// implementation if needed.  l and cmdCons must not be nil.
func chooseSystem(ctx context.Context, l *slog.Logger, cmdCons executil.CommandConstructor) {
	sys := service.ChosenSystem()
	if sys.String() == "linux-systemd" {
		service.ChooseSystem(&systemdSystem{System: sys, cmdCons: cmdCons})
		l.DebugContext(ctx, "using custom systemd system")

		return
	}

	l.DebugContext(ctx, "using default system", "system_description", sys.String())
}

// systemdSystem is a wrapper for a [service.System] that returns the custom
// implementation of the [service.Service] interface.
type systemdSystem struct {
	// System must have an unexported type *service.linuxSystemService.
	service.System

	// cmdCons is used to run external commands.  It must not be nil.
	cmdCons executil.CommandConstructor
}

// type check
var _ service.System = (*systemdSystem)(nil)

// New implements the [service.System] interface for *systemdSystem.  i and c
// must not be nil.
func (sys *systemdSystem) New(i service.Interface, c *service.Config) (s service.Service, err error) {
	s, err = sys.System.New(i, c)
	if err != nil {
		// Don't wrap the error to keep it as close to the original one as
		// possible.
		return s, err
	}

	return &systemdService{
		cmdCons:  sys.cmdCons,
		Service:  s,
		unitName: fmt.Sprintf("%s.service", c.Name),
	}, nil
}

// type check
var _ service.Service = (*systemdService)(nil)

// systemdService is a wrapper for a systemd [service.Service] that enriches the
// service status information.
type systemdService struct {
	// cmdCons is used to run external commands.  It must not be nil.
	cmdCons executil.CommandConstructor

	// Service is expected to have an unexported type *service.systemd.
	service.Service

	// unitName stores the name of the systemd daemon.
	unitName string
}

// type check
var _ service.Service = (*systemdService)(nil)

// Status implements the [service.Service] interface for *systemdService.
func (s *systemdService) Status() (status service.Status, err error) {
	const systemctlCmd = "systemctl"

	var (
		systemctlArgs   = []string{"show", s.unitName}
		systemctlStdout bytes.Buffer
	)

	// TODO(f.setrakov):  Consider streaming the output if needed.  Using
	// [io.Pipe] here is unnecessary; it complicates lifecycle management
	// because the output must be read concurrently, and the PipeWriter must be
	// explicitly closed to signal EOF.  Since this command's output is small, a
	// bytes.Buffer via [executil.Run] is sufficient.
	err = executil.Run(
		// TODO(f.setrakov):  Pass context.
		context.TODO(),
		s.cmdCons,
		&executil.CommandConfig{
			Path:   systemctlCmd,
			Args:   systemctlArgs,
			Stdout: &systemctlStdout,
		},
	)
	if err != nil {
		return service.StatusUnknown, fmt.Errorf("executing command: %w", err)
	}

	status, err = parseSystemctlShow(&systemctlStdout)
	if err != nil {
		return service.StatusUnknown, fmt.Errorf("parsing command output: %w", err)
	}

	return status, nil
}

// Searched property names.  See man systemctl(1).
const (
	propNameLoadState   = "LoadState"
	propNameActiveState = "ActiveState"
	propNameSubState    = "SubState"
)

// parseSystemctlShow parses the output of the systemctl show command.  It
// expects the key=value pairs separated by newlines.
func parseSystemctlShow(output io.Reader) (status service.Status, err error) {
	var loadState, activeState, subState string

	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		line := scanner.Text()

		propName, propValue, ok := strings.Cut(line, "=")
		if !ok {
			return service.StatusUnknown, fmt.Errorf("unexpected line format: %q", line)
		}

		switch propName {
		case propNameLoadState:
			loadState = propValue
		case propNameActiveState:
			activeState = propValue
		case propNameSubState:
			subState = propValue
		default:
			// Go on.
		}
	}
	if err = scanner.Err(); err != nil {
		return service.StatusUnknown, err
	}

	return statusFromState(loadState, activeState, subState)
}

// statusFromState returns the service status based on the systemctl state
// property values.
func statusFromState(loadState, activeState, subState string) (status service.Status, err error) {
	// Desired property values.  See man systemctl(1).
	const (
		propValueLoadStateNotFound   = "not-found"
		propValueActiveStateActive   = "active"
		propValueActiveStateInactive = "inactive"
		propValueSubStateAutoRestart = "auto-restart"
	)

	switch {
	case loadState == propValueLoadStateNotFound:
		return service.StatusUnknown, service.ErrNotInstalled
	case activeState == propValueActiveStateActive:
		return service.StatusRunning, nil
	case activeState == propValueActiveStateInactive:
		return service.StatusStopped, nil
	case subState == propValueSubStateAutoRestart:
		return statusRestartOnFail, nil
	default:
		return service.StatusUnknown, fmt.Errorf(
			"unexpected state: %s=%q, %s=%q, %s=%q",
			propNameLoadState, loadState,
			propNameActiveState, activeState,
			propNameSubState, subState,
		)
	}
}
