package main

import (
	"sync/atomic"
	"time"

	"netflix-household-validator/internal/config"
	"netflix-household-validator/internal/emailprocessor"
	imapclient "netflix-household-validator/internal/imap"
	"netflix-household-validator/internal/logging"
	"netflix-household-validator/internal/models"
	"netflix-household-validator/internal/netflix"
)

var imapFailureCount atomic.Int32

const failureSleepDuration = 30 * time.Minute

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		logging.Log.Fatalf("Error reading configuration file: %v", err)
	}

	logging.Log.Infof("Starting Netflix email verification process, refresh every %s", cfg.Email.RefreshTime)

	// Start background cleanup for Rod temp directories
	netflix.StartCleanup()

	// Initialize Netflix service
	browser := netflix.NewRodBrowser()
	netflixService := netflix.NewService(browser, cfg)

	for {
		fetchAndProcessEmails(cfg, netflixService)
		time.Sleep(cfg.Email.RefreshTime)
	}
}

// fetchAndProcessEmails connects to the IMAP server, retrieves unseen emails, and processes them
func fetchAndProcessEmails(cfg *models.Config, netflixService *netflix.Service) {
	client := imapclient.NewStandardClient()

	// Connect
	if err := client.Connect(cfg.Email.Imap); err != nil {
		handleIMAPFailure(err)
		return
	}
	defer func(client *imapclient.StandardClient) {
		_ = client.Close()
	}(client)

	// Reset failure count on successful connection
	imapFailureCount.Store(0)

	// Login
	if err := client.Login(cfg.Email.Login, cfg.Email.Password); err != nil {
		logging.Log.Errorf("Login error: %v", err)
		return
	}

	// Select mailbox
	if err := client.SelectMailbox(cfg.Email.MailBox); err != nil {
		logging.Log.Errorf("Folder selection error: %v", err)
		return
	}

	// List unseen emails from last 15 minutes
	uids, err := client.ListUnseenUIDs(emailprocessor.EmailValidityWindow)
	if err != nil {
		logging.Log.Errorf("Error searching for recent emails: %v", err)
		return
	}

	if len(uids) == 0 {
		return
	}

	// Create email processor
	processor := emailprocessor.NewProcessor(client, netflixService)

	// Process all unseen emails
	for _, uid := range uids {
		if err := processor.ProcessEmail(uid); err != nil {
			logging.Log.Errorf("Error processing email UID %d: %v", uid, err)
		}
	}
}

// handleIMAPFailure increments the failure count and implements an exponential backoff strategy
func handleIMAPFailure(err error) {
	failures := imapFailureCount.Add(1)
	logging.Log.Errorf("IMAP connection error: %v", err)

	if failures >= 5 {
		base := 5 * time.Minute
		maxSteps := int32(10)

		n := failures - 5
		if n > maxSteps {
			n = maxSteps
		}

		backoff := base * time.Duration(1<<n)
		if backoff > failureSleepDuration {
			backoff = failureSleepDuration
		}

		logging.Log.Warnf("IMAP failed %d times, waiting %s before next attempt", failures, backoff)
		time.Sleep(backoff)
	}
}
