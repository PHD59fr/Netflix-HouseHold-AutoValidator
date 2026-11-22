package models

import (
	"fmt"
	"net"
	"strings"
)

// Config represents the application configuration
type Config struct {
	Email         EmailConfig `yaml:"email"`
	TargetFrom    string      `yaml:"targetFrom"`
	TargetSubject string      `yaml:"targetSubject"`
}

// EmailConfig represents IMAP email configuration
type EmailConfig struct {
	Imap     string `yaml:"imap"`
	Login    string `yaml:"login"`
	Password string `yaml:"password"`
	MailBox  string `yaml:"mailbox"`
}

// Validate refuses to start with an incomplete configuration instead of
// spinning forever on connection errors with empty credentials.
func (c *Config) Validate() error {
	required := []struct {
		name  string
		value string
	}{
		{"email.imap", c.Email.Imap},
		{"email.login", c.Email.Login},
		{"email.password", c.Email.Password},
		{"email.mailbox", c.Email.MailBox},
		{"targetFrom", c.TargetFrom},
		{"targetSubject", c.TargetSubject},
	}

	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("missing required configuration: %s", field.name)
		}
	}

	if _, _, err := net.SplitHostPort(c.Email.Imap); err != nil {
		return fmt.Errorf("invalid email.imap %q: expected host:port: %w", c.Email.Imap, err)
	}

	return nil
}
