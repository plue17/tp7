package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestLoadCreatesDefaultConfigFile verifies that Load writes a default config
// file when it does not yet exist, and that the written YAML contains the
// transcriber section.
func TestLoadCreatesDefaultConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tp7", "tp7.yaml")

	cfg, err := Load(path, false)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// Returned config must have a Transcriber field (zero value is fine).
	_ = cfg.Transcriber

	// The file must have been created.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config file was not created: %v", err)
	}

	// The written YAML must contain the transcriber key.
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("could not parse written config: %v", err)
	}

	if _, ok := raw["transcriber"]; !ok {
		t.Errorf("written config does not contain 'transcriber' key; got keys: %v", keys(raw))
	}
}

func keys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
