package config

import (
	"os"

	"netflix-household-validator/internal/models"

	"gopkg.in/yaml.v2"
)

// Load reads the configuration from the specified YAML file and returns a Config struct
func Load(filepath string) (*models.Config, error) {
	var cfg models.Config

	// Load YAML (optional)
	if filepath != "" {
		data, err := os.ReadFile(filepath)
		if err != nil {
			return nil, err
		}

		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	}

	// Override with environment variables if set
	overrideFromEnv(&cfg)

	return &cfg, nil
}

// overrideFromEnv checks for specific environment variables and overrides the corresponding fields in the Config struct if they are set
func overrideFromEnv(cfg *models.Config) {
	setString(&cfg.TargetFrom, "TARGET_FROM")
	setString(&cfg.TargetSubject, "TARGET_SUBJECT")

	setString(&cfg.Email.Imap, "EMAIL_IMAP")
	setString(&cfg.Email.Login, "EMAIL_LOGIN")
	setString(&cfg.Email.Password, "EMAIL_PASSWORD")
	setString(&cfg.Email.MailBox, "EMAIL_MAILBOX")
}

// setString checks if the specified environment variable is set and not empty, and if so, assigns its value to the provided string pointer
func setString(field *string, envKey string) {
	if v, ok := os.LookupEnv(envKey); ok && v != "" {
		*field = v
	}
}
