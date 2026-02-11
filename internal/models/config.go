package models

import "time"

// Config represents the application configuration
type Config struct {
	NetflixAuth     []NetflixAccount `yaml:"netflixAuth"`
	FilterByAccount bool             `yaml:"filterByAccount"`
	Email           EmailConfig      `yaml:"email"`
	TargetFrom      string           `yaml:"targetFrom"`
	TargetSubject   string           `yaml:"targetSubject"`
}

// EmailConfig represents IMAP email configuration
type EmailConfig struct {
	Imap        string        `yaml:"imap"`
	Login       string        `yaml:"login"`
	Password    string        `yaml:"password"`
	RefreshTime time.Duration `yaml:"refreshTime"`
	MailBox     string        `yaml:"mailbox"`
}

// NetflixAccount represents Netflix account credentials
type NetflixAccount struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
}
