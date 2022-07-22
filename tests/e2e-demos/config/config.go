package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

func LoadTestGeneratorConfig(configPath string) (WorkflowSpec, error) {
	c := WorkflowSpec{}
	// Open config file
	file, err := os.Open(filepath.Clean(configPath))
	if err != nil {
		return c, err
	}

	// Init new YAML decode
	d := yaml.NewDecoder(file)

	// Start YAML decoding from file
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	return c, nil
}
