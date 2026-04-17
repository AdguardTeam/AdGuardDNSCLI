//go:build darwin

package cmd

import (
	"context"
	"io"
	"os"
	"path"
	"testing"

	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/osutil/executil"
	"github.com/AdguardTeam/golibs/testutil"
	"github.com/AdguardTeam/golibs/testutil/fakeos/fakeexec"
	"github.com/kardianos/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testServiceName is a common service name for tests.
const testServiceName = "AdGuardDNSCLI"

// testLogger is a common logger for tests.
var testLogger = slogutil.NewDiscardLogger()

// newTestCmdConstructor is a helper that creates a new command constructor. The
// returned constructor creates [fakeexec.Command] instances that print the
// given body to the command's standard output and return the error.
func newTestCmdConstructor(
	tb testing.TB,
	body string,
	returnErr error,
) (c executil.CommandConstructor) {
	tb.Helper()

	onNew := func(
		_ context.Context,
		conf *executil.CommandConfig,
	) (c executil.Command, err error) {
		cmd := fakeexec.NewCommand()
		cmd.OnStart = func(_ context.Context) (err error) {
			_, err = io.WriteString(conf.Stdout, body)
			require.NoError(tb, err)

			return returnErr
		}

		cmd.OnWait = func(_ context.Context) (err error) { return nil }

		return cmd, nil
	}

	return &fakeexec.CommandConstructor{
		OnNew: onNew,
	}
}

func TestDarwinService_Status(t *testing.T) {
	t.Parallel()

	plistDir := t.TempDir()
	plistPath := path.Join(plistDir, testServiceName+".plist")
	file, err := os.Create(plistPath)
	require.NoError(t, err)

	testutil.CleanupAndRequireSuccess(t, file.Close)

	testCases := []struct {
		cmdErr     error
		name       string
		body       string
		wantStatus service.Status
	}{{
		name: "running",
		body: `
		{
			"PID" = 12345;
		};`,
		cmdErr:     nil,
		wantStatus: service.StatusRunning,
	}, {
		name: "restarting",
		body: `
		{
			"foo" = "bar";
		};`,
		cmdErr:     nil,
		wantStatus: statusRestartOnFail,
	}, {
		name:       "stopped",
		body:       "",
		cmdErr:     errors.Error("launchctl error"),
		wantStatus: service.StatusStopped,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newDarwinService(&darwinServiceConfig{
				logger:   testLogger,
				cmdCons:  newTestCmdConstructor(t, tc.body, tc.cmdErr),
				plistDir: plistDir,
				name:     testServiceName,
			})

			var status service.Status
			status, err = svc.Status()
			require.NoError(t, err)

			assert.Equal(t, tc.wantStatus, status)
		})
	}
}

func TestDarwinService_Status_notInstalled(t *testing.T) {
	t.Parallel()

	svc := newDarwinService(&darwinServiceConfig{
		logger:  testLogger,
		cmdCons: executil.EmptyCommandConstructor{},
		name:    testServiceName,
	})

	status, err := svc.Status()
	assert.Equal(t, service.StatusUnknown, status)
	assert.ErrorIs(t, err, service.ErrNotInstalled)
}
