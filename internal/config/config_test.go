package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoad(t *testing.T) {
	yamlContent := `email:
  imap: "imap.test.com:993"
  login: "test@example.com"
  password: "testpass"
  mailbox: "INBOX"
targetFrom: "info@test.com"
targetSubject: "Test Subject"
`

	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func(name string) {
		_ = os.Remove(name)
	}(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(yamlContent)); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Email.Imap != "imap.test.com:993" {
		t.Errorf("Expected imap 'imap.test.com:993', got '%s'", cfg.Email.Imap)
	}

	if cfg.TargetFrom != "info@test.com" {
		t.Errorf("Expected targetFrom 'info@test.com', got '%s'", cfg.TargetFrom)
	}
}

func TestLoadRejectsIncompleteConfiguration(t *testing.T) {
	valid := `email:
  imap: "imap.test.com:993"
  login: "test@example.com"
  password: "testpass"
  mailbox: "INBOX"
targetFrom: "info@test.com"
targetSubject: "Test Subject"
`

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"empty file", "", "email.imap"},
		{"missing imap", strings.Replace(valid, `imap: "imap.test.com:993"`, `imap: ""`, 1), "email.imap"},
		{"missing password", strings.Replace(valid, `password: "testpass"`, `password: ""`, 1), "email.password"},
		{"missing mailbox", strings.Replace(valid, `mailbox: "INBOX"`, `mailbox: ""`, 1), "email.mailbox"},
		{"missing targetFrom", strings.Replace(valid, `targetFrom: "info@test.com"`, `targetFrom: ""`, 1), "targetFrom"},
		{"missing targetSubject", strings.Replace(valid, `targetSubject: "Test Subject"`, `targetSubject: ""`, 1), "targetSubject"},
		{"invalid imap address", strings.Replace(valid, `imap: "imap.test.com:993"`, `imap: "imap.test.com"`, 1), "invalid email.imap"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.content))
			if err == nil {
				t.Fatalf("Load() succeeded, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load() error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadWithoutFileUsesEnvironmentOnly(t *testing.T) {
	env := map[string]string{
		"EMAIL_IMAP":     "imap.test.com:993",
		"EMAIL_LOGIN":    "test@example.com",
		"EMAIL_PASSWORD": "testpass",
		"EMAIL_MAILBOX":  "INBOX",
		"TARGET_FROM":    "info@test.com",
		"TARGET_SUBJECT": "Test Subject",
	}
	for key, value := range env {
		t.Setenv(key, value)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error: %v", err)
	}
	if cfg.Email.Imap != "imap.test.com:993" || cfg.TargetSubject != "Test Subject" {
		t.Errorf("unexpected config from environment: %+v", cfg)
	}
}

func TestLoadEnvironmentOverridesMissingValue(t *testing.T) {
	content := `email:
  imap: "imap.test.com:993"
  login: "test@example.com"
  password: "testpass"
  mailbox: "INBOX"
targetFrom: "info@test.com"
`
	t.Setenv("TARGET_SUBJECT", "Subject from environment")

	cfg, err := Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.TargetSubject != "Subject from environment" {
		t.Errorf("TargetSubject = %q, want environment value", cfg.TargetSubject)
	}
}
