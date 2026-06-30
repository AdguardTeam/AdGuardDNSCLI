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
func newHumanID(s string) (id HumanID, err error) {
	err = validate.InRange("human id", len(s), minHumanIDLen, maxHumanIDLen)
	if err != nil {
		return "", err
	}

	err = netutil.ValidateHostnameLabel(s)
	if err != nil {
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

	err = netutil.ValidateHostnameLabel(idStr)
	if err != nil {
		// Don't wrap the error, because it is informative enough as is.
		return "", err
	}

	if i := strings.Index(idStr, "---"); i >= 0 {
		return "", fmt.Errorf("at index %d: max 2 consecutive hyphens are allowed", i)
	}

	return HumanID(idStr), nil
}

// NewProfileID converts s into a [ProfileID] and makes sure that it's valid.
func NewProfileID(s string) (id ProfileID, err error) {
	s = strings.ToLower(s)

	err = validate.InRange("profile id", len(s), 1, ProfileIDLen)
	if err != nil {
		return "", err
	}

	for i, r := range s {
		if r < '!' || r > '~' {
			return "", fmt.Errorf("bad char %q at index %d", r, i)
		}
	}

	return ProfileID(s), nil
}

// deviceTypes is the set of valid values of [DeviceType].
var deviceTypes = container.NewMapSet[DeviceType](
	"win",
	"adr",
	"mac",
	"ios",
	"lnx",
	"rtr",
	"stv",
	"gam",
	"otr",
)

// NewDeviceType converts s into a [DeviceType] and makes sure that it's valid.
func NewDeviceType(s string) (dt DeviceType, err error) {
	dt = DeviceType(s)
	if !deviceTypes.Has(dt) {
		return "", fmt.Errorf("device type: %w: %q", errors.ErrBadEnumValue, s)
	}

	return dt, nil
}
