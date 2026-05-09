package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// defaults returns a Config populated with built-in default values.
func defaults() *Config {
	lib := ""
	if home, err := os.UserHomeDir(); err == nil {
		lib = filepath.Join(home, "tp7")
	}
	return &Config{
		MTPDevice: MTPDeviceConfig{
			VendorID:  "2367",
			ProductID: "0019",
		},
		Library: LibraryConfig{
			Path: lib,
		},
	}
}

// Load reads a YAML configuration file and returns the parsed Config.
// If path does not exist and was not explicitly provided (explicit=false),
// the default config is written to path and the built-in defaults are returned.
func Load(path string, explicit bool) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if !explicit && os.IsNotExist(err) {
			cfg := defaults()
			if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr == nil {
				if wf, wErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600); wErr == nil {
					_ = yaml.NewEncoder(wf).Encode(cfg)
					_ = wf.Close()
				}
			}
			return cfg, nil
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
	MTPDevice   MTPDeviceConfig   `yaml:"mtp_device"`
	Library     LibraryConfig     `yaml:"library"`
	Transcriber TranscriberConfig `yaml:"transcriber"`
}

// TranscriberConfig describes the remote transcriber service.
type TranscriberConfig struct {
	// Host is the IP address or hostname of the transcriber service.
	// Leave empty to disable remote transcription.
	Host string `yaml:"host"`
	// Port is the TCP port of the transcriber service. Defaults to 0 (unset).
	Port int `yaml:"port"`
}

// LibraryConfig describes the on-disk library of topics.
type LibraryConfig struct {
	// Path is the root directory of the library.
	// Defaults to $HOME/tp7 if not set in the YAML.
	Path string `yaml:"path"`
}

// MTPDeviceConfig describes the MTP device to search for.
type MTPDeviceConfig struct {
	// VendorID is the USB vendor ID in hex (e.g. "2367").
	VendorID string `yaml:"vendor_id"`
	// ProductID is the USB product ID in hex (e.g. "0019").
	ProductID string `yaml:"product_id"`
}
