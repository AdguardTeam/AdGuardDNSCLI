package cmd

import (
	"fmt"

	"github.com/AdguardTeam/AdGuardDNSCLI/internal/client"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/validate"
)

// autodeviceConfig is a configuration for an autodevice feature for a specific
// upstream group.
type autodeviceConfig struct {
	ProfileID  string `yaml:"profile_id"`
	DeviceType string `yaml:"device_type"`
	Enabled    bool   `yaml:"enabled"`
}

// type check
var _ validate.Interface = (*autodeviceConfig)(nil)

// Validate implements the [validate.Interface] interface for *autodeviceConfig.
func (c *autodeviceConfig) Validate() (err error) {
	if c == nil {
		return errors.ErrNoValue
	} else if !c.Enabled {
		return nil
	}

	var errs []error

	if _, err = client.NewProfileID(c.ProfileID); err != nil {
		errs = append(errs, fmt.Errorf("profile_id: %w", err))
	}

	if _, err = client.NewDeviceType(c.DeviceType); err != nil {
		errs = append(errs, fmt.Errorf("device_type: %w", err))
	}

	return errors.Join(errs...)
}
