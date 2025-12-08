package main

import (
	"os"
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

	for {
		fetchLastUnseenEmail(config)
		time.Sleep(config.Email.RefreshTime)
	}
}
