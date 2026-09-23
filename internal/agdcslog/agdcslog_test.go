package agdcslog_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/agdcslog"
	"github.com/AdguardTeam/golibs/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTimeout is the common timeout for tests.
const testTimeout = 1 * time.Second

// testServiceName is the service name for integration tests.
const testServiceName = "AdGuardDNSCLITest"

// requireIntegration skips the test unless TEST_AGDCSLOG is set to "1".
func requireIntegration(tb testing.TB) {
	tb.Helper()

	const envName = "TEST_AGDCSLOG"

	switch v := os.Getenv(envName); v {
	case "":
		tb.Skipf("skipping: %s is not set", envName)
	case "0":
		tb.Skip("skipping: integration tests are disabled")
	case "1":
		// Go on.
	default:
		tb.Skipf(`skipping: %s must be "1" or "0", got %q`, envName, v)
	}
}

// requireExec skips the test if the executable is not in the PATH environment
// variable or the provided path does not exist.
func requireExec(tb testing.TB, name string) {
	tb.Helper()

	_, err := exec.LookPath(name)
	if err != nil {
		tb.Skipf("skipping: executable %q not found on PATH", name)
	}
}

// integrationSystemLogger returns a slog.Logger configured for system logging
// in integration tests.
func integrationSystemLogger(tb testing.TB) (l *slog.Logger) {
	tb.Helper()

	ctx := testutil.ContextWithTimeout(tb, testTimeout)
	sl, err := agdcslog.NewSystemLogger(ctx, testServiceName)
	require.NoError(tb, err)

	testutil.CleanupAndRequireSuccess(tb, sl.Close)

	h := agdcslog.NewSyslogHandler(sl, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	return slog.New(h)
}

// requireEventuallyFound writes a message to the system log and observes it
// using the provided finder function.
func requireEventuallyFound(
	tb testing.TB,
	cmdLogReader string,
	find func(ctx context.Context, msg string) (ok bool, err error),
) {
	tb.Helper()

	requireIntegration(tb)
	requireExec(tb, cmdLogReader)

	l := integrationSystemLogger(tb)
	msg := strconv.FormatInt(time.Now().UnixNano(), 10)

	ctx := testutil.ContextWithTimeout(tb, testTimeout)
	l.InfoContext(ctx, msg)

	require.EventuallyWithT(tb, func(ct *assert.CollectT) {
		findCtx, cancel := context.WithTimeout(ctx, testTimeout)
		defer cancel()

		ok, err := find(findCtx, msg)
		require.NoError(ct, err)

		assert.True(ct, ok)
	}, testTimeout, testTimeout/10)
}

// testLogger is a mock implementation of [agdcslog.SystemLogger] interface for
// tests.
//
// TODO(e.burkov):  Move into a separate package with testing utilities.
type testLogger struct {
	onDebug   func(msg string) (err error)
	onInfo    func(msg string) (err error)
	onWarning func(msg string) (err error)
	onError   func(msg string) (err error)
	onClose   func() (err error)
}

// newTestLogger returns a new mock logger with all its methods set to panic.
func newTestLogger() (l *testLogger) {
	return &testLogger{
		onInfo: func(msg string) (_ error) {
			panic(testutil.UnexpectedCall(msg))
		},
		onWarning: func(msg string) (_ error) {
			panic(testutil.UnexpectedCall(msg))
		},
		onError: func(msg string) (_ error) {
			panic(testutil.UnexpectedCall(msg))
		},
		onDebug: func(msg string) (_ error) {
			panic(testutil.UnexpectedCall(msg))
		},
		onClose: func() (_ error) {
			panic(testutil.UnexpectedCall())
		},
	}
}

// type check
var _ agdcslog.SystemLogger = (*testLogger)(nil)

// Debug implements [agdcslog.SystemLogger] interface for *testLogger.
func (l *testLogger) Debug(msg string) (err error) {
	return l.onDebug(msg)
}

// Info implements [agdcslog.SystemLogger] interface for *testLogger.
func (l *testLogger) Info(msg string) (err error) {
	return l.onInfo(msg)
}

// Warning implements [agdcslog.SystemLogger] interface for *testLogger.
func (l *testLogger) Warning(msg string) (err error) {
	return l.onWarning(msg)
}

// Error implements [agdcslog.SystemLogger] interface for *testLogger.
func (l *testLogger) Error(msg string) (err error) {
	return l.onError(msg)
}

// Close implements [agdcslog.SystemLogger] interface for *testLogger.
func (l *testLogger) Close() (err error) {
	return l.onClose()
}

func TestSyslogHandler_Handle(t *testing.T) {
	var (
		mu     = sync.Mutex{}
		output = &bytes.Buffer{}
	)

	outputWrite := func(msg string) (err error) {
		mu.Lock()
		defer mu.Unlock()

		output.WriteString(msg + "\n")

		return nil
	}

	l := newTestLogger()
	l.onInfo = outputWrite
	l.onWarning = outputWrite
	l.onError = outputWrite
	l.onDebug = outputWrite

	handler := agdcslog.NewSyslogHandler(l, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	const testMsg = "test message"

	testCases := []struct {
		name  string
		want  string
		attrs []slog.Attr
		level slog.Level
	}{{
		name:  "level_info",
		level: slog.LevelInfo,
		want:  `level=INFO msg="test message"`,
	}, {
		name:  "level_warn",
		level: slog.LevelWarn,
		attrs: []slog.Attr{},
		want:  `level=WARN msg="test message"`,
	}, {
		name:  "level_err",
		level: slog.LevelError,
		attrs: []slog.Attr{},
		want:  `level=ERROR msg="test message"`,
	}, {
		name:  "level_debug",
		level: slog.LevelDebug,
		attrs: []slog.Attr{},
		want:  `level=DEBUG msg="test message"`,
	}, {
		name:  "level_custom",
		level: slog.Level(-8),
		attrs: []slog.Attr{},
		want:  `level=DEBUG-4 msg="test message"`,
	}, {
		name:  "level_info_with_args",
		level: slog.LevelInfo,
		attrs: []slog.Attr{
			slog.Int("int", 123),
			slog.String("string", "abc"),
		},
		want: `level=INFO msg="test message" int=123 string=abc`,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := slog.NewRecord(time.Now(), tc.level, testMsg, 0)
			r.AddAttrs(tc.attrs...)

			err := handler.Handle(context.Background(), r)
			require.NoError(t, err)

			line, err := output.ReadString('\n')
			require.NoError(t, err)

			line = line[:len(line)-1]
			assert.Equal(t, tc.want, line)
		})
	}
}

func TestSyslogHandler_Handle_race(t *testing.T) {
	var (
		mu     = sync.Mutex{}
		output = &bytes.Buffer{}
	)

	l := newTestLogger()
	l.onInfo = func(msg string) (err error) {
		mu.Lock()
		defer mu.Unlock()

		output.WriteString(msg + "\n")

		return nil
	}

	h := agdcslog.NewSyslogHandler(l, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	logger := slog.New(h)

	// Test with multiple goroutines to be sure there are no races.
	const numGoroutine = 1_000

	wg := &sync.WaitGroup{}
	for range numGoroutine {
		wg.Go(func() {
			logger.Info("test message", "attr", "abc")
		})
	}

	wg.Wait()

	const wantMsg = `level=INFO msg="test message" attr=abc` + "\n"

	var num int
	for s := range strings.Lines(output.String()) {
		assert.Equal(t, wantMsg, s)
		num++
	}

	assert.Equal(t, numGoroutine, num)
}

func BenchmarkSyslogHandler_Handle(b *testing.B) {
	l := newTestLogger()
	l.onInfo = func(_ string) (_ error) { return nil }

	h := agdcslog.NewSyslogHandler(l, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	ctx := context.Background()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	r.AddAttrs(
		slog.Int("int", 123),
		slog.String("string", "abc"),
	)

	var err error
	b.ReportAllocs()
	for b.Loop() {
		err = h.Handle(ctx, r)
	}

	require.NoError(b, err)

	// Most recent results:
	//
	//	goos: darwin
	//	goarch: arm64
	//	pkg: github.com/AdguardTeam/AdGuardDNSCLI/internal/agdcslog
	//	cpu: Apple M4 Pro
	//	BenchmarkSyslogHandler_Handle-14    	 5660894	       194.3 ns/op	      64 B/op	       1 allocs/op
}
