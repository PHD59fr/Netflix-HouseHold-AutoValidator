package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v2"
)

type Config struct {
	NetflixAuth     []NetflixAccount `yaml:"netflixAuth"`
	FilterByAccount bool             `yaml:"filterByAccount"`
	Email           EmailConfig      `yaml:"email"`
	TargetFrom      string           `yaml:"targetFrom"`
	TargetSubject   string           `yaml:"targetSubject"`
}

type EmailConfig struct {
	Imap        string        `yaml:"imap"`
	Login       string        `yaml:"login"`
	Password    string        `yaml:"password"`
	RefreshTime time.Duration `yaml:"refreshTime"` // ex: "30s", "1m"
	MailBox     string        `yaml:"mailbox"`
}

type NetflixAccount struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
}

var activeRodSessions atomic.Int32

func main() {
	configFile, err := os.ReadFile("config.yaml")
	if err != nil {
		log.Fatalf("Error reading configuration file: %v", err)
	}

	var config Config
	if err := yaml.Unmarshal(configFile, &config); err != nil {
		log.Fatalf("Error parsing configuration file: %v", err)
	}

	log.Infof("Starting Netflix email verification process, refresh every %s", config.Email.RefreshTime)

	go cleanupTempDirs()

	for {
		fetchLastUnseenEmail(config)
		time.Sleep(config.Email.RefreshTime)
	}
}

func cleanupTempDirs() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if activeRodSessions.Load() > 0 {
			log.Info("Skipping /tmp cleanup: active Rod sessions detected")
			continue
		}

		pattern := filepath.Join(os.TempDir(), "rod-netflix-*")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			log.WithError(err).Warn("Failed to glob temp directories")
			continue
		}

		for _, dir := range matches {
			if err := os.RemoveAll(dir); err != nil {
				log.WithError(err).Warnf("Failed to remove temp dir: %s", dir)
			} else {
				log.Infof("Cleaned up temp dir: %s", dir)
			}
		}
	}
}
