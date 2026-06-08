package client_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/client"
	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/testutil"
	"github.com/AdguardTeam/golibs/timeutil"
	"github.com/stretchr/testify/assert"
)

// testLogger is a logger used for tests.
var testLogger = slogutil.NewDiscardLogger()

// testTimeout is the common timeout for tests.
const testTimeout = 1 * time.Second

// testSearch is a case of searching through a particular clients set.
type testSearch struct {
	want client.Client
	addr netip.Addr
}

func TestStorage_ByAddr(t *testing.T) {
	t.Parallel()

	cli1Addr1 := netip.MustParseAddr("192.0.2.0")
	cli1Pref1 := netip.PrefixFrom(cli1Addr1, 31)

	cli1Addr2 := netip.MustParseAddr("192.0.2.4")
	cli1Pref2 := netip.PrefixFrom(cli1Addr2, 30)

	cli2Addr1 := netip.MustParseAddr("198.51.100.0")
	cli2Pref1 := netip.PrefixFrom(cli2Addr1, 32)

	absentAddr := cli2Addr1.Next()

	cli1 := client.NewStaticClient(&proxy.CustomUpstreamConfig{})
	cli2 := client.NewStaticClient(&proxy.CustomUpstreamConfig{})

	conf := &client.DefaultStorageConfig{
		Logger: testLogger,
		Clock:  timeutil.SystemClock{},
		Static: map[netip.Prefix]*client.StaticClient{
			cli1Pref1: cli1,
			cli1Pref2: cli1,
			cli2Pref1: cli2,
		},
	}

	cs := client.NewDefaultStorage(conf)

	testutil.CleanupAndRequireSuccess(t, func() (err error) {
		ctx := testutil.ContextWithTimeout(t, testTimeout)

		return cs.Shutdown(ctx)
	})

	testCases := []struct {
		name     string
		searches []testSearch
	}{{
		name: "empty",
		searches: []testSearch{{
			addr: absentAddr,
			want: nil,
		}, {
			addr: cli1Addr2.Prev(),
			want: nil,
		}},
	}, {
		name: "single",
		searches: []testSearch{{
			addr: cli1Addr1,
			want: cli1,
		}, {
			addr: cli1Addr2,
			want: cli1,
		}, {
			addr: cli2Addr1.Next(),
			want: nil,
		}, {
			addr: absentAddr,
			want: nil,
		}},
	}, {
		name: "multiple",
		searches: []testSearch{{
			addr: cli1Addr1,
			want: cli1,
		}, {
			addr: cli1Addr2,
			want: cli1,
		}, {
			addr: cli2Addr1,
			want: cli2,
		}, {
			addr: absentAddr,
			want: nil,
		}},
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runSearchesTests(t, cs, tc.searches)
		})
	}
}

// runSearchesTests runs tests on a particular clients set, stored in searches.
// t and cs must not be nil.
func runSearchesTests(t *testing.T, cs client.Storage, searches []testSearch) {
	t.Helper()

	for _, sc := range searches {
		t.Run(sc.addr.String(), func(t *testing.T) {
			t.Parallel()

			ctx := testutil.ContextWithTimeout(t, testTimeout)
			c, ok := cs.ByAddr(ctx, sc.addr)
			assert.Equal(t, sc.want != nil, ok)
			assert.Equal(t, sc.want, c)
		})
	}
}
