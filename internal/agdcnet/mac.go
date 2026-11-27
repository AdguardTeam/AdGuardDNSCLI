package agdcnet

import (
	"encoding"
	"fmt"
	"net"

	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/netutil"
)

// MAC address length discriminators.
const (
	macTypeEUI48 uint8 = iota + 1
	macTypeEUI64
	macTypeInfiniBand
)

// MAC address lengths for different address types.
const (
	macLenEUI48      = 6
	macLenEUI64      = 8
	macLenInfiniBand = 20
)

// lenByMacType returns the length of the MAC address based on the length
// discriminator.  macType must be one of the following values:
//   - [macTypeEUI48]
//   - [macTypeEUI64]
//   - [macTypeInfiniBand]
func lenByMacType(macType uint8) (l int) {
	switch macType {
	case macTypeEUI48:
		return macLenEUI48
	case macTypeEUI64:
		return macLenEUI64
	case macTypeInfiniBand:
		return macLenInfiniBand
	default:
		panic(fmt.Errorf("unsupported mac type: %d: %w", macType, errors.ErrBadEnumValue))
	}
}

// macTypeByLength returns the macType corresponding to the given MAC address
// length.  length must be one of the following values:
//   - [macLenEUI48]
//   - [macLenEUI64]
//   - [macLenInfiniBand]
func macTypeByLength(length int) (mc uint8) {
	switch length {
	case macLenEUI48:
		return macTypeEUI48
	case macLenEUI64:
		return macTypeEUI64
	case macLenInfiniBand:
		return macTypeInfiniBand
	default:
		err := &netutil.LengthError{
			Length:  length,
			Kind:    netutil.AddrKindMAC,
			Allowed: []int{macLenEUI48, macLenEUI64, macLenInfiniBand},
		}

		panic(fmt.Errorf("unsupported mac address length: %w", err))
	}
}

// macLength defines the fixed size of the byte array used to store a
// MAC address.
const macLength = 21

// MAC represents a MAC address stored as a fixed-size byte array.  This value
// is comparable and can be used as a key in maps.  The supported MAC address
// formats are:
//   - EUI-48
//   - EUI-64
//   - InfiniBand
//
// Zero value of MAC represents an unset MAC address.
type MAC struct {
	// address is a MAC address.  The address scheme consists of a first byte
	// indicating the length discriminator, followed by the MAC address bytes.
	// The discriminator might be one of the following values:
	//  - 0: the MAC address is considered unset.
	//  - [macTypeEUI48]: 6 bytes are used for the MAC address (EUI-48).
	//  - [macTypeEUI64]: 8 bytes are used for the MAC address (EUI-64).
	//  - [macTypeInfiniBand]: 20 bytes are used for the MAC address (InfiniBand).
	//
	// The remaining bytes (from index 1 onwards) store the actual MAC address
	// bytes, depending on the length discriminator.
	address [macLength]byte
}

// type check
var _ fmt.Stringer = MAC{}

// String implements the [fmt.Stringer] interface for MAC.
func (m MAC) String() (s string) {
	if m == (MAC{}) {
		return ""
	}

	l := lenByMacType(m.address[0])

	return net.HardwareAddr(m.address[1 : 1+l]).String()
}

// type check
var _ encoding.TextUnmarshaler = (*MAC)(nil)

// UnmarshalText implements the [encoding.TextUnmarshaler] interface for *MAC.
//
// TODO(m.kazantsev):  Add benchmarks and ensure there no allocations during
// parsing.
func (m *MAC) UnmarshalText(text []byte) (err error) {
	macStr := string(text)

	// TODO(m.kazantsev):  Consider improving error format.
	parsedMAC, err := net.ParseMAC(macStr)
	if err != nil {
		return fmt.Errorf("parsing mac address: %w", err)
	}

	l := len(parsedMAC)
	d := macTypeByLength(l)

	m.address = [macLength]byte{}
	m.address[0] = d
	copy(m.address[1:], parsedMAC)

	return nil
}

// NewMACFromHardwareAddr converts a [net.HardwareAddr] to a MAC.
func NewMACFromHardwareAddr(hwAddr net.HardwareAddr) (m MAC, err error) {
	err = netutil.ValidateMAC(hwAddr)
	if err != nil {
		return MAC{}, fmt.Errorf("invalid mac address: %w", err)
	}

	addrLen := len(hwAddr)
	d := macTypeByLength(addrLen)

	m = MAC{}
	m.address[0] = d
	copy(m.address[1:], hwAddr)

	return m, nil
}
