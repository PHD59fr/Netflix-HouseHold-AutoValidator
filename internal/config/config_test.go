package config

import (
	"os"
	"testing"
)

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
