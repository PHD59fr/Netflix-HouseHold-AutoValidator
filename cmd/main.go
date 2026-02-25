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

	// Create persistent IMAP client
	client := imapclient.NewStandardClient()
	var connected bool

	for {
		// Check if connection is healthy, reconnect if necessary
		if !connected || !client.IsHealthy() {
			if connected {
				logging.Log.Warn("IMAP connection lost, reconnecting...")
				_ = client.Close()
			}

			if err := connectAndAuthenticate(client, cfg); err != nil {
				handleIMAPFailure(err)
				time.Sleep(cfg.Email.RefreshTime)
				continue
			}

			connected = true
			imapFailureCount.Store(0)
		}

		// Process emails with the persistent connection
		fetchAndProcessEmails(client, netflixService, cfg)

		time.Sleep(cfg.Email.RefreshTime)
	}
}

// connectAndAuthenticate establishes connection and authenticates with IMAP server
func connectAndAuthenticate(client *imapclient.StandardClient, cfg *models.Config) error {
	// Connect
	if err := client.Connect(cfg.Email.Imap); err != nil {
		return err
	}

	// Login
	if err := client.Login(cfg.Email.Login, cfg.Email.Password); err != nil {
		logging.Log.Errorf("Login error: %v", err)
		_ = client.Close()
		return err
	}

	logging.Log.Info("IMAP connection established successfully")
	return nil
}

// fetchAndProcessEmails retrieves unseen emails and processes them using the existing connection
func fetchAndProcessEmails(client *imapclient.StandardClient, netflixService *netflix.Service, cfg *models.Config) {
	startTime := time.Now()

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

	logging.Log.Infof("Found %d unseen email(s) to process", len(uids))

	// Initialize statistics
	stats := emailprocessor.ProcessingStats{
		Total: len(uids),
	}

	// Create email processor
	processor := emailprocessor.NewProcessor(client, netflixService)

	// Process all unseen emails
	for _, uid := range uids {
		handled, ignored, err := processor.ProcessEmail(uid)
		if err != nil {
			logging.Log.Errorf("Error processing email UID %d: %v", uid, err)
			stats.Failed++
			continue
		}

		if handled {
			stats.Processed++
		} else if ignored {
			stats.Ignored++
		}
	}

	// Log summary
	duration := time.Since(startTime)
	logging.Log.Infof(
		"Processing cycle completed in %v - Total: %d | Processed: %d | Ignored: %d | Failed: %d",
		duration.Round(time.Millisecond),
		stats.Total,
		stats.Processed,
		stats.Ignored,
		stats.Failed,
	)
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
