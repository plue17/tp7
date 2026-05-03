package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// defaults returns a Config populated with built-in default values.
func defaults() *Config {
	return &Config{
		MTPDevice: MTPDeviceConfig{
			VendorID:  "2367",
			ProductID: "0019",
		},
	}
}

// Load reads a YAML configuration file and returns the parsed Config.
// If path does not exist and was not explicitly provided (explicit=false),
// the built-in defaults are returned silently.
func Load(path string, explicit bool) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if !explicit && os.IsNotExist(err) {
			return defaults(), nil
		}
		return nil, fmt.Errorf("opening configuration: %w", err)
	}
	defer f.Close()

	cfg := defaults()
	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("parsing configuration: %w", err)
	}

	return cfg, nil
}

// Config is the top-level configuration structure.
type Config struct {
	MTPDevice MTPDeviceConfig `yaml:"mtp_device"`
}

// MTPDeviceConfig describes the MTP device to search for.
type MTPDeviceConfig struct {
	// VendorID is the USB vendor ID in hex (e.g. "2367").
	VendorID string `yaml:"vendor_id"`
	// ProductID is the USB product ID in hex (e.g. "0019").
	ProductID string `yaml:"product_id"`
}
