package agdcnet

import (
	"net"
	"testing"

	"github.com/AdguardTeam/golibs/testutil"
	"github.com/stretchr/testify/assert"
)

// Common test MAC string values.
const (
	testMACEUI48Str      = "00:01:02:03:04:05"
	testMACEUI64Str      = "00:01:02:03:04:05:06:07"
	testMACInfiniBandStr = "00:01:02:03:04:05:06:07:08:09:0a:0b:0c:0d:0e:0f:10:11:12:13"
)

// Common test MAC values.
var (
	testMACEUI48 = MAC{address: [21]byte{
		macTypeEUI48, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05,
	}}
	testMACEUI64 = MAC{
		address: [21]byte{
			macTypeEUI64, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		},
	}
	testMACInfiniBand = MAC{
		address: [21]byte{
			macTypeInfiniBand, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
			0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13,
		},
	}
)

func TestMAC_String(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		want string
		m    MAC
	}{{
		name: "unset_mac",
		m:    MAC{},
		want: "",
	}, {
		name: "mac_eui48",
		m:    testMACEUI48,
		want: testMACEUI48Str,
	}, {
		name: "mac_eui64",
		m:    testMACEUI64,
		want: testMACEUI64Str,
	}, {
		name: "mac_infiniband",
		m:    testMACInfiniBand,
		want: testMACInfiniBandStr,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.m.String()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMAC_UnmarshalText(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		wantErrMsg string
		text       []byte
		want       MAC
	}{{
		name:       "unset_mac",
		text:       []byte(""),
		want:       MAC{},
		wantErrMsg: `parsing mac address: invalid MAC address`,
	}, {
		name:       "invalid_mac",
		text:       []byte{0x04, 0x02},
		want:       MAC{},
		wantErrMsg: "parsing mac address: address \x04\x02: invalid MAC address",
	}, {
		name:       "mac_eui48",
		text:       []byte(testMACEUI48Str),
		want:       testMACEUI48,
		wantErrMsg: "",
	}, {
		name:       "mac_eui64",
		text:       []byte(testMACEUI64Str),
		want:       testMACEUI64,
		wantErrMsg: "",
	}, {
		name:       "mac_infiniband",
		text:       []byte(testMACInfiniBandStr),
		want:       testMACInfiniBand,
		wantErrMsg: "",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got MAC
			err := got.UnmarshalText(tc.text)
			testutil.AssertErrorMsg(t, tc.wantErrMsg, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNewMACFromHardwareAddr(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		wantErrMsg string
		hwAddr     net.HardwareAddr
		want       MAC
	}{{
		name:       "empty_mac",
		hwAddr:     []byte{},
		want:       MAC{},
		wantErrMsg: `invalid mac address: bad mac address "": mac address is empty`,
	}, {
		name:       "invalid_mac",
		hwAddr:     net.HardwareAddr{0x04, 0x02},
		want:       MAC{},
		wantErrMsg: `invalid mac address: bad mac address "04:02": bad mac address length 2, allowed: [6 8 20]`,
	}, {
		name:       "mac_eui48",
		hwAddr:     net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
		want:       testMACEUI48,
		wantErrMsg: "",
	}, {
		name:       "mac_eui64",
		hwAddr:     net.HardwareAddr{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		want:       testMACEUI64,
		wantErrMsg: "",
	}, {
		name: "mac_infiniband",
		hwAddr: net.HardwareAddr{
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09,
			0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13,
		},
		want:       testMACInfiniBand,
		wantErrMsg: "",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewMACFromHardwareAddr(tc.hwAddr)
			testutil.AssertErrorMsg(t, tc.wantErrMsg, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
