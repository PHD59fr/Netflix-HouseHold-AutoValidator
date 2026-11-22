package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"netflix-household-validator/internal/config"
	"netflix-household-validator/internal/emailprocessor"
	imapclient "netflix-household-validator/internal/imap"
	"netflix-household-validator/internal/logging"
	"netflix-household-validator/internal/models"
	"netflix-household-validator/internal/netflix"
)

var imapFailureCount atomic.Int32

const (
	failureSleepDuration = 30 * time.Minute
	maxEmailsPerCycle    = 100
)

func main() {
	// The default path also matches the Docker image, where WORKDIR is "/".
	configPath := flag.String("config", "config.yaml", "path to the YAML configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if errors.Is(err, os.ErrNotExist) && *configPath == "config.yaml" {
		// The default file is optional when all settings come from the
		// environment. An explicitly named alternate file must still exist.
		cfg, err = config.Load("")
	}
	if err != nil {
		logging.Log.Fatalf("Invalid configuration: %v", err)
	}

	logging.Log.Info("Starting Netflix email verification process (IMAP IDLE mode)")

	// Start background cleanup for Rod temp directories
	netflix.StartCleanup()

	// Initialize Netflix service
	browser := netflix.NewRodBrowser()
	netflixService := netflix.NewService(browser, cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Create persistent IMAP client
	client := imapclient.NewStandardClient()
	defer func() { _ = client.Close() }()

	connected := false
	processed := make(map[uint32]struct{})

	for {
		if ctx.Err() != nil {
			logging.Log.Info("Shutting down gracefully")
			return
		}

		if !connected {
			if err := connectAndAuthenticate(ctx, client, cfg); err != nil {
				handleIMAPFailure(ctx, err)
				continue
			}
			connected = true
			processed = make(map[uint32]struct{})
		}

		// Process all emails that arrived before or during connection setup.
		// This also drains emails that arrived while the browser was busy.
		more, err := fetchAndProcessEmails(ctx, client, netflixService, processed)
		if err != nil {
			connected = false
			_ = client.Close()

			if errors.Is(err, context.Canceled) {
				logging.Log.Info("Shutting down gracefully")
				return
			}

			handleIMAPFailure(ctx, err)
			continue
		}
		if more {
			// Resume immediately with the same UID set so earlier ignored or
			// failed messages cannot starve the rest of the mailbox.
			continue
		}
		processed = make(map[uint32]struct{})

		// Block until the server notifies us of new mail via IMAP IDLE, or
		// until the periodic safety poll asks for a new scan.
		if err := client.WaitForNewMail(ctx); err != nil {
			connected = false
			_ = client.Close()

			if errors.Is(err, context.Canceled) {
				logging.Log.Info("Shutting down gracefully")
				return
			}

			handleIMAPFailure(ctx, err)
			continue
		}

		// A full IDLE session (or a periodic poll) completed without error:
		// the connection is stable, so the backoff can be reset.
		imapFailureCount.Store(0)
	}
}

// connectAndAuthenticate establishes connection and authenticates with IMAP server
func connectAndAuthenticate(ctx context.Context, client *imapclient.StandardClient, cfg *models.Config) error {
	// Connect
	if err := client.Connect(ctx, cfg.Email.Imap); err != nil {
		return err
	}

	// Login
	if err := client.Login(ctx, cfg.Email.Login, cfg.Email.Password); err != nil {
		_ = client.Close()
		return err
	}

	// Select the mailbox once per connection. Re-selecting on every cycle
	// makes go-imap report the SELECT's EXISTS response as a mailbox update,
	// which would immediately wake IDLE again and create a busy loop.
	if err := client.SelectMailbox(cfg.Email.MailBox); err != nil {
		_ = client.Close()
		return fmt.Errorf("select mailbox %q: %w", cfg.Email.MailBox, err)
	}

	logging.Log.Info("IMAP connection established successfully")
	return nil
}

// fetchAndProcessEmails retrieves unseen emails and processes them using the existing connection.
// It keeps scanning until no new UID appears, so an email that arrives while a
// previous email is being handled by the browser is not missed.
func fetchAndProcessEmails(
	ctx context.Context,
	client imapclient.Client,
	netflixService *netflix.Service,
	processed map[uint32]struct{},
) (more bool, err error) {
	startTime := time.Now()

	// The mailbox is already selected by connectAndAuthenticate. Selecting it
	// again here would make go-imap report the SELECT's EXISTS response as a
	// mailbox update and immediately wake IDLE, creating a busy loop.

	// Create email processor
	processor := emailprocessor.NewProcessor(client, netflixService)
	stats := emailprocessor.ProcessingStats{}
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}

		// List unseen emails from last 15 minutes
		uids, err := client.ListUnseenUIDs(emailprocessor.EmailValidityWindow)
		if err != nil {
			return false, fmt.Errorf("list unseen emails: %w", err)
		}

		batch := make([]uint32, 0, len(uids))
		for _, uid := range uids {
			if _, alreadyProcessed := processed[uid]; !alreadyProcessed {
				batch = append(batch, uid)
			}
		}

		if len(batch) == 0 {
			break
		}

		remaining := maxEmailsPerCycle - stats.Total
		if remaining == 0 {
			logging.Log.Warnf(
				"Reached the limit of %d emails for this cycle; continuing immediately",
				maxEmailsPerCycle,
			)
			more = true
			break
		}
		if len(batch) > remaining {
			batch = batch[:remaining]
			more = true
		}

		stats.Total += len(batch)
		logging.Log.Infof("Found %d new unseen email(s) to process", len(batch))

		for _, uid := range batch {
			if err := ctx.Err(); err != nil {
				return false, err
			}

			processed[uid] = struct{}{}

			handled, ignored, err := processor.ProcessEmail(ctx, uid)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return false, err
				}

				logging.Log.Errorf("Error processing email UID %d: %v", uid, err)
				stats.Failed++
				continue
			}

			switch {
			case handled:
				stats.Processed++
			case ignored:
				stats.Ignored++
			}
		}
		if more {
			break
		}
	}

	if stats.Total == 0 {
		return more, nil
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

	return more, nil
}

// handleIMAPFailure increments the failure count and implements an exponential backoff strategy.
// First failure reconnects immediately; subsequent failures use exponential backoff.
// The wait is interrupted when ctx is cancelled so SIGTERM does not hang for up
// to 30 minutes.
func handleIMAPFailure(ctx context.Context, err error) {
	failures := imapFailureCount.Add(1)
	logging.Log.Errorf("IMAP connection error: %v", err)

	if failures == 1 {
		logging.Log.Warnf("IMAP failed, reconnecting immediately...")
		return
	}

	var backoff time.Duration
	if failures < 5 {
		backoff = 10 * time.Second
	} else {
		base := 5 * time.Minute
		maxSteps := int32(10)

		n := failures - 5
		if n > maxSteps {
			n = maxSteps
		}

		backoff = base * time.Duration(1<<n)
		if backoff > failureSleepDuration {
			backoff = failureSleepDuration
		}
	}

	logging.Log.Warnf("IMAP failed %d times, waiting %s before next attempt", failures, backoff)

	timer := time.NewTimer(backoff)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
