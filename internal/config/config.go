package config

import (
	"os"

	"netflix-household-validator/internal/models"

	"gopkg.in/yaml.v2"
)

// Load reads the configuration from the specified YAML file and returns a Config struct
func Load(filepath string) (*models.Config, error) {
	configFile, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	var config models.Config
	if err := yaml.Unmarshal(configFile, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
