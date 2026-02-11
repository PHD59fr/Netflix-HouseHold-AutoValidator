package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	yamlContent := `email:
  imap: "imap.test.com:993"
  login: "test@example.com"
  password: "testpass"
  refreshTime: 30s
  mailbox: "INBOX"
targetFrom: "info@test.com"
targetSubject: "Test Subject"
filterByAccount: true
netflixAuth:
  - email: user1@example.com
    password: pass1
  - email: user2@example.com
    password: pass2
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

	if cfg.Email.RefreshTime != 30*time.Second {
		t.Errorf("Expected refreshTime 30s, got %v", cfg.Email.RefreshTime)
	}

	if cfg.TargetFrom != "info@test.com" {
		t.Errorf("Expected targetFrom 'info@test.com', got '%s'", cfg.TargetFrom)
	}

	if !cfg.FilterByAccount {
		t.Error("Expected filterByAccount to be true")
	}

	if len(cfg.NetflixAuth) != 2 {
		t.Errorf("Expected 2 Netflix accounts, got %d", len(cfg.NetflixAuth))
	}

	if cfg.NetflixAuth[0].Email != "user1@example.com" {
		t.Errorf("Expected first account email 'user1@example.com', got '%s'", cfg.NetflixAuth[0].Email)
	}
}
