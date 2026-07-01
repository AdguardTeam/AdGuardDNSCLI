package client

import (
	"fmt"
	"strings"

	"github.com/AdguardTeam/golibs/container"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/netutil"
	"github.com/AdguardTeam/golibs/validate"
)

// HumanID is an identifier for DNS client.  It must be unique for each client
// among a single [Storage].
type HumanID string

// ProfileID is the ID of a profile.
//
// TODO(e.burkov):  Consider moving to agdc.
type ProfileID string

// DeviceType is a type of a device.
//
// TODO(e.burkov):  Consider moving to agdc.
type DeviceType string

// Constants for [HumanID] validation.
const (
	// maxHumanIDLen is the maximum length of HumanID.
	maxHumanIDLen = netutil.MaxDomainLabelLen - DeviceTypeLen - ProfileIDLen - 2*len("-")

	// minHumanIDLen is the minimum length of HumanID.
	//
	// NOTE:  Keep in sync with https://github.com/AdguardTeam/AdGuardDNS/blob/3f26cca7e094801647ea6e93503d6ed61c545737/internal/agd/humanid.go#L23.
	minHumanIDLen = 1
)

// Constants for upstream templates.
//
// TODO(e.burkov):  Consider moving to agdc.
const (
	// DeviceTypeLen is the length of DeviceType.
	DeviceTypeLen = 3

	// ProfileIDLen is the length of ProfileID.
	ProfileIDLen = 8
)

// newHumanID converts a simple string into a [HumanID] and makes sure that
// it's valid.
//
// NOTE:  Keep in sync with https://github.com/AdguardTeam/AdGuardDNS/blob/3f26cca7e094801647ea6e93503d6ed61c545737/internal/agd/humanid.go#L45.
func newHumanID(s string) (id HumanID, err error) {
	err = validate.InRange("human id length", len(s), minHumanIDLen, maxHumanIDLen)
	if err != nil {
		// Don't wrap the error, because the caller should do that.
		return "", err
	}

	err = netutil.ValidateHostnameLabel(s)
	if err != nil {
		// Don't wrap the error, because the caller should do that.
		return "", err
	}

	if i := strings.Index(s, "---"); i >= 0 {
		return "", fmt.Errorf("at index %d: max 2 consecutive hyphens are allowed", i)
	}

	return HumanID(s), nil
}

// fqdnToHumanID converts a FQDN string into HumanID, if possible.
func fqdnToHumanID(fqdn string) (id HumanID, err error) {
	domain := strings.TrimSuffix(fqdn, ".")

	err = validate.NoLessThan("domain", len(domain), minHumanIDLen)
	if err != nil {
		// Don't wrap the error, because it is informative enough as is.
		return "", err
	}

	if len(domain) > maxHumanIDLen {
		domain = strings.TrimSuffix(domain[:maxHumanIDLen], ".")
	}

	idStr := strings.ReplaceAll(domain, ".", "-")

	return newHumanID(idStr)
}

// NewProfileID converts s into a ProfileID and makes sure that it's valid.
//
// NOTE:  Keep in sync with https://github.com/AdguardTeam/AdGuardDNS/blob/3f26cca7e094801647ea6e93503d6ed61c545737/internal/agd/profile.go#L114.
func NewProfileID(s string) (id ProfileID, err error) {
	if err = validate.Equal("profile id length", len(s), ProfileIDLen); err != nil {
		return "", fmt.Errorf("bad profile id %q: %w", s, err)
	}

	// For now, allow only the printable, non-whitespace ASCII characters.
	// Technically we only need to exclude carriage return and line feed
	// characters, but let's be more strict just in case.
	for i, r := range s {
		if r < '!' || r > '~' {
			return "", fmt.Errorf("bad profile id: bad char %q at index %d", r, i)
		}
	}

	return ProfileID(s), nil
}

// deviceTypes is the set of valid values of [DeviceType].
//
// NOTE:  Keep in sync with https://github.com/AdguardTeam/AdGuardDNS/blob/3f26cca7e094801647ea6e93503d6ed61c545737/internal/agd/devicetype.go#L32.
var deviceTypes = container.NewMapSet[DeviceType](
	"adr",
	"gam",
	"ios",
	"lnx",
	"mac",
	"otr",
	"rtr",
	"stv",
	"win",
)

// NewDeviceType converts s into a [DeviceType] and makes sure that it's valid.
func NewDeviceType(s string) (dt DeviceType, err error) {
	dt = DeviceType(s)
	if !deviceTypes.Has(dt) {
		return "", fmt.Errorf("device type: %w: %q", errors.ErrBadEnumValue, s)
	}

	return dt, nil
}
