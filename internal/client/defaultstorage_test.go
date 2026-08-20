package client_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"runtime"
	"testing"
	"time"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/client"
	"github.com/AdguardTeam/dnsproxy/dnsproxytest"
	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/AdguardTeam/golibs/testutil"
	"github.com/AdguardTeam/golibs/testutil/servicetest"
	"github.com/AdguardTeam/golibs/timeutil"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testUpstreamURL is a common upstream URL for tests.
const testUpstreamURL = "https://dns.example/dns-query"

const (
	// testTLD is a common TLD for domains in tests.
	testTLD = "example"

	// testDomainCommon is a common question domain for tests.
	testDomainCommon = "question." + testTLD

	// testDomainSub is a common subdomain of testQuestionDomain for tests.
	testDomainSub = "www." + testDomainCommon

	// testDomainOther is another common question domain for tests.
	testDomainOther = "other." + testTLD

	// testDomainUnmatched is a common unmatched question domain for tests.
	testDomainUnmatched = "unmatched.test"
)

// testAutodeviceUpstreamConfig is a common autodevice upstream configuration
// for tests.
var testAutodeviceUpstreamConfig = &client.AutodeviceUpstreamConfig{
	UpstreamTemplate: errors.Must(url.Parse(testUpstreamURL)),
	DeviceType:       errors.Must(client.NewDeviceType("mac")),
	ProfileID:        errors.Must(client.NewProfileID("abcdefgh")),
}

// testAutodeviceClientConfig is a common autodevice client configuration for
// tests.
var testAutodeviceClientConfig = client.AutodeviceClientConfig{
	"": testAutodeviceUpstreamConfig,
}

func TestDefaultStorage_Get_static(t *testing.T) {
	t.Parallel()

	const (
		upstreamURLAnother = "tls://dns.example/dns-query"
		upstreamURLDomain  = "quic://dns.example"
	)

	addr1Cli1 := netip.MustParseAddr("192.0.2.0")
	pref1Cli1 := netip.PrefixFrom(addr1Cli1, 31)

	addr2Cli1 := netip.MustParseAddr("192.0.2.4")
	pref2Cli1 := netip.PrefixFrom(addr2Cli1, 30)

	addr1Cli2 := netip.MustParseAddr("198.51.100.0")
	pref1Cli2 := netip.PrefixFrom(addr1Cli2, 32)

	addrAbsent := addr1Cli2.Next()

	custUpsConf1 := newTestStaticClientConf(t, testUpstreamURL)
	cli1 := client.NewStaticClient(custUpsConf1)

	custUpsConf2 := newTestStaticClientConf(t, upstreamURLAnother)
	cli2 := client.NewStaticClient(custUpsConf2)

	custUpsConf3 := newTestStaticClientConf(t, upstreamURLDomain)
	cli3 := client.NewStaticClient(custUpsConf3)

	testCases := []struct {
		static   map[netip.Prefix]client.StaticClientConfig
		name     string
		searches []testSearch
	}{{
		name:   "empty",
		static: nil,
		searches: []testSearch{{
			addr:   addrAbsent,
			domain: testDomainCommon,
			want:   nil,
		}, {
			addr:   addr2Cli1.Prev(),
			domain: testDomainCommon,
			want:   nil,
		}},
	}, {
		name: "single",
		static: map[netip.Prefix]client.StaticClientConfig{
			pref1Cli1: {
				"":               cli1,
				"example":        cli2,
				testDomainCommon: cli3,
			},
			pref2Cli1: {"": cli1},
		},
		searches: []testSearch{{
			addr:   addr1Cli1,
			domain: testDomainCommon,
			want:   custUpsConf3,
		}, {
			addr:   addr1Cli1,
			domain: testDomainSub,
			want:   custUpsConf3,
		}, {
			addr:   addr1Cli1,
			domain: testDomainOther,
			want:   custUpsConf2,
		}, {
			addr:   addr1Cli1,
			domain: testDomainUnmatched,
			want:   custUpsConf1,
		}, {
			addr:   addr2Cli1,
			domain: testDomainCommon,
			want:   custUpsConf1,
		}, {
			addr:   addr1Cli2.Next(),
			domain: testDomainCommon,
			want:   nil,
		}, {
			addr:   addrAbsent,
			domain: testDomainCommon,
			want:   nil,
		}},
	}, {
		name: "multiple",
		static: map[netip.Prefix]client.StaticClientConfig{
			pref1Cli1: {
				"":               cli1,
				testDomainCommon: cli3,
			},
			pref2Cli1: {"": cli1},
			pref1Cli2: {"": cli2},
		},
		searches: []testSearch{{
			addr:   addr1Cli1,
			domain: testDomainCommon,
			want:   custUpsConf3,
		}, {
			addr:   addr1Cli1,
			domain: testDomainSub,
			want:   custUpsConf3,
		}, {
			addr:   addr2Cli1,
			domain: testDomainCommon,
			want:   custUpsConf1,
		}, {
			addr:   addr1Cli2,
			domain: testDomainCommon,
			want:   custUpsConf2,
		}, {
			addr:   addrAbsent,
			domain: testDomainCommon,
			want:   nil,
		}},
	}}

	for _, tc := range testCases {
		cs := client.NewDefaultStorage(&client.DefaultStorageConfig{
			Logger:              testLogger,
			Clock:               timeutil.SystemClock{},
			UpstreamConstructor: client.DefaultUpstreamConstructor{},
			Static:              tc.static,
		})

		// Shutdown closes upstreams, which are shared among subtests, so run it
		// in the end of the main test.
		servicetest.RequireRun(t, cs, testTimeout)

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runSearchesTests(t, cs, tc.searches)
		})
	}
}

func TestDefaultStorage_Get_autodevice(t *testing.T) {
	t.Parallel()

	addrSpecific := testIPv4
	prefSpecific := netip.PrefixFrom(addrSpecific, addrSpecific.BitLen())

	addrDefault := netip.MustParseAddr("198.51.100.7")

	source := &testHumanIDSource{
		onIdentify: func(_ context.Context, addr netip.Addr) (id *client.ValidHumanID, err error) {
			idStr := hex.EncodeToString(addr.AsSlice())

			return &client.ValidHumanID{
				ID:    client.HumanID(idStr),
				Until: time.Now().Add(testValidUntilIvl),
			}, nil
		},
	}

	idSpecific := errors.Must(source.Identify(t.Context(), addrSpecific)).ID
	idDefault := errors.Must(source.Identify(t.Context(), addrDefault)).ID

	upsCons := newComparableUpstreamConstructor()

	upsConf := testAutodeviceUpstreamConfig

	extIDSpecific := fmt.Sprintf("%s-%s-%s", upsConf.DeviceType, upsConf.ProfileID, idSpecific)
	upsURLSpecific := upsConf.UpstreamTemplate.JoinPath(extIDSpecific).String()
	upsConfSpecific := &proxy.UpstreamConfig{
		Upstreams: []upstream.Upstream{
			errors.Must(upsCons.AddressToUpstream(upsURLSpecific, upsConf.Options)),
		},
	}
	custConfSpecific := proxy.NewCustomUpstreamConfig(upsConfSpecific, false, 0, false)

	extIDDefault := fmt.Sprintf("%s-%s-%s", upsConf.DeviceType, upsConf.ProfileID, idDefault)
	upsURLDefault := upsConf.UpstreamTemplate.JoinPath(extIDDefault).String()
	upsConfDefault := &proxy.UpstreamConfig{
		Upstreams: []upstream.Upstream{
			errors.Must(upsCons.AddressToUpstream(upsURLDefault, upsConf.Options)),
		},
	}
	custConfDefault := proxy.NewCustomUpstreamConfig(upsConfDefault, false, 0, false)

	testCases := []struct {
		autodevice map[netip.Prefix]client.AutodeviceClientConfig
		name       string
		searches   []testSearch
	}{{
		name: "general_only",
		autodevice: map[netip.Prefix]client.AutodeviceClientConfig{
			{}: testAutodeviceClientConfig,
		},
		searches: []testSearch{{
			addr:   addrDefault,
			domain: testDomainCommon,
			want:   custConfDefault,
		}, {
			addr:   addrSpecific,
			domain: testDomainCommon,
			want:   custConfSpecific,
		}},
	}, {
		name: "both",
		autodevice: map[netip.Prefix]client.AutodeviceClientConfig{
			prefSpecific: testAutodeviceClientConfig,
			{}:           testAutodeviceClientConfig,
		},
		searches: []testSearch{{
			addr:   addrSpecific,
			domain: testDomainCommon,
			want:   custConfSpecific,
		}, {
			addr:   addrDefault,
			domain: testDomainCommon,
			want:   custConfDefault,
		}},
	}, {
		name: "specific_only",
		autodevice: map[netip.Prefix]client.AutodeviceClientConfig{
			prefSpecific: testAutodeviceClientConfig,
		},
		searches: []testSearch{{
			addr:   addrDefault,
			domain: testDomainCommon,
			want:   nil,
		}, {
			addr:   addrSpecific,
			domain: testDomainCommon,
			want:   custConfSpecific,
		}},
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cs := client.NewDefaultStorage(&client.DefaultStorageConfig{
				Logger:              testLogger,
				Clock:               timeutil.SystemClock{},
				HumanIDSource:       source,
				UpstreamConstructor: upsCons,
				Identifiable:        netutil.SubnetSetFunc(client.IsIdentifiable),
				Autodevice:          tc.autodevice,
				// Never cleanup in this test.
				CleanupIvl: 10 * testTimeout,
			})
			servicetest.RequireRun(t, cs, testTimeout)

			runSearchesTests(t, cs, tc.searches)
		})
	}
}

func TestDefaultStorage_Get_autodeviceCache(t *testing.T) {
	t.Parallel()

	now := time.Now()
	clock, afterCh := newTestClock(t, &now)

	source := &testHumanIDSource{
		onIdentify: func(_ context.Context, addr netip.Addr) (id *client.ValidHumanID, err error) {
			return &client.ValidHumanID{
				ID:    client.HumanID(hex.EncodeToString(addr.AsSlice())),
				Until: now.Add(testValidUntilIvl),
			}, nil
		},
	}
	cs := client.NewDefaultStorage(&client.DefaultStorageConfig{
		Logger:              testLogger,
		Clock:               clock,
		HumanIDSource:       source,
		UpstreamConstructor: client.DefaultUpstreamConstructor{},
		Identifiable:        netutil.SubnetSetFunc(client.IsIdentifiable),
		Autodevice: map[netip.Prefix]client.AutodeviceClientConfig{
			{}: testAutodeviceClientConfig,
		},
		CleanupIvl: testValidUntilIvl / 2,
	})
	servicetest.RequireRun(t, cs, testTimeout)

	var first client.Client
	var ok bool

	require.True(t, t.Run("first", func(t *testing.T) {
		ctx := testutil.ContextWithTimeout(t, testTimeout)

		first, ok = cs.Get(ctx, testIPv4, testDomainCommon)
		require.True(t, ok)

		assert.NotNil(t, first)
	}))

	require.True(t, t.Run("cache", func(t *testing.T) {
		ctx := testutil.ContextWithTimeout(t, testTimeout)

		var cached client.Client
		cached, ok = cs.Get(ctx, testIPv4, testDomainCommon)
		require.True(t, ok)

		assert.Same(t, first, cached)
	}))

	var refreshed client.Client

	now = now.Add(testValidUntilIvl + 1*time.Second)
	testutil.RequireSend(t, afterCh, now, testTimeout)

	require.True(t, t.Run("expire", func(t *testing.T) {
		ctx := testutil.ContextWithTimeout(t, testTimeout)

		refreshed, ok = cs.Get(ctx, testIPv4, testDomainCommon)
		require.True(t, ok)

		assert.NotSame(t, first, refreshed)
	}))

	require.True(t, t.Run("cache_after_expire", func(t *testing.T) {
		ctx := testutil.ContextWithTimeout(t, testTimeout)

		var latest client.Client
		latest, ok = cs.Get(ctx, testIPv4, testDomainCommon)
		require.True(t, ok)

		assert.Same(t, refreshed, latest)
		assert.NotSame(t, first, refreshed)
	}))
}

func TestDefaultStorage_SetFinalizer(t *testing.T) {
	t.Parallel()

	const localTestTimeout = 5 * testTimeout

	now := time.Now()
	clock, afterCh := newTestClock(t, &now)

	source := &testHumanIDSource{
		onIdentify: func(_ context.Context, addr netip.Addr) (id *client.ValidHumanID, err error) {
			return &client.ValidHumanID{
				ID:    client.HumanID(hex.EncodeToString(addr.AsSlice())),
				Until: now.Add(testValidUntilIvl),
			}, nil
		},
	}

	closeCh := make(chan struct{})
	onAddrToUps := func(addr string, _ *upstream.Options) (up upstream.Upstream, err error) {
		return &dnsproxytest.Upstream{
			OnAddress:  func() (addr string) { return "" },
			OnExchange: func(_ *dns.Msg) (resp *dns.Msg, err error) { return nil, nil },
			OnClose: func() (err error) {
				_, _ = testutil.RequireReceive(testutil.NewPanicT(t), closeCh, localTestTimeout)

				return nil
			},
		}, nil
	}
	upsCons := &testUpstreamConstructor{
		onAddressToUpstream: onAddrToUps,
	}

	storage := client.NewDefaultStorage(&client.DefaultStorageConfig{
		Logger:              testLogger,
		Clock:               clock,
		HumanIDSource:       source,
		UpstreamConstructor: upsCons,
		Identifiable:        netutil.SubnetSetFunc(client.IsIdentifiable),
		Autodevice: map[netip.Prefix]client.AutodeviceClientConfig{
			{}: testAutodeviceClientConfig,
		},
		CleanupIvl: testValidUntilIvl / 2,
	})
	servicetest.RequireRun(t, storage, testTimeout)

	const clientsCount = 10

	require.True(t, t.Run("create_clients", func(t *testing.T) {
		addr := testIPv4

		for range clientsCount {
			client, ok := storage.Get(t.Context(), addr, testDomainCommon)
			require.True(t, ok)

			assert.NotNil(t, client)

			addr = addr.Next()
		}
	}))

	require.True(t, t.Run("cleanup_clients", func(t *testing.T) {
		now = now.Add(testValidUntilIvl + 1*time.Second)
		testutil.RequireSend(t, afterCh, now, testTimeout)

		for range clientsCount {
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				runtime.GC()

				testutil.RequireSend(ct, afterCh, now, localTestTimeout)
			}, localTestTimeout, testTimeout/5)
		}
	}))
}

// testSearch is a case of searching through a particular clients set.
type testSearch struct {
	want   *proxy.CustomUpstreamConfig
	addr   netip.Addr
	domain string
}

// runSearchesTests runs tests on a particular clients set, stored in searches.
// t and cs must not be nil.
func runSearchesTests(t *testing.T, cs client.Storage, searches []testSearch) {
	t.Helper()

	for _, sc := range searches {
		testName := fmt.Sprintf("%s_%s", sc.addr, sc.domain)

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			ctx := testutil.ContextWithTimeout(t, testTimeout)
			c, ok := cs.Get(ctx, sc.addr, sc.domain)
			require.Equal(t, sc.want != nil, ok)

			if sc.want == nil {
				return
			}

			require.NotNil(t, c)

			assert.Equal(t, sc.want, c.Upstreams())
		})
	}
}

// newTestStaticClientConf creates a new static client upstream configuration
// for tests.
func newTestStaticClientConf(tb testing.TB, upstreamURL string) (conf *proxy.CustomUpstreamConfig) {
	tb.Helper()

	upsConf, err := proxy.ParseUpstreamsConfig([]string{upstreamURL}, nil)
	require.NoError(tb, err)

	conf = proxy.NewCustomUpstreamConfig(upsConf, false, 0, false)

	return conf
}
