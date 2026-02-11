package emailprocessor

import (
	"netflix-household-validator/internal/mailparse"
	"time"

	imapclient "netflix-household-validator/internal/imap"
	"netflix-household-validator/internal/logging"
	"netflix-household-validator/internal/models"

	"netflix-household-validator/internal/netflix"
)

// Netflix typically sends verification emails immediately, so a 15-minute window is reasonable to account for delays without processing stale emails.
const EmailValidityWindow = 15 * time.Minute

type Processor struct {
	imapClient     imapclient.Client
	netflixService *netflix.Service
}

// NewProcessor creates a new Processor instance with the provided IMAP client and Netflix service
func NewProcessor(imapClient imapclient.Client, netflixService *netflix.Service) *Processor {
	return &Processor{
		imapClient:     imapClient,
		netflixService: netflixService,
	}
}

// ProcessEmail orchestrates the complete email processing workflow:
// fetch → parse → validate age → handle → mark as seen
func (p *Processor) ProcessEmail(uid uint32) error {
	// Fetch message from IMAP
	msg, err := p.imapClient.FetchMessage(uid)
	if err != nil {
		return err
	}

	// Parse email to normalized structure
	email, err := mailparse.Parse(msg)
	if err != nil {
		logging.Log.WithField("trace_id", "unknown").Errorf("Error parsing email UID %d: %v", uid, err)
		return err
	}

	locallog := logging.Log.WithField("trace_id", email.TraceID)

	// Validate email age (15 minutes window)
	if !p.isEmailValid(email) {
		locallog.Infof("Message UID %d is older than %v (date: %v), skipping", uid, EmailValidityWindow, email.InternalDate)
		return nil
	}

	// Handle email with Netflix service (filters, browser automation)
	handled := p.netflixService.HandleEmail(email)

	// Mark as seen only if successfully handled
	if handled {
		if err := p.imapClient.MarkSeen(uid); err != nil {
			locallog.Errorf("Error marking message UID %d as seen: %v", uid, err)
		}
	}

	return nil
}

// isEmailValid checks if email is within the validity window (15 minutes)
func (p *Processor) isEmailValid(email *models.Email) bool {
	return p.isEmailValidAt(email, time.Now())
}

// isEmailValidAt allows testing with a fixed "now" time for deterministic unit tests
func (p *Processor) isEmailValidAt(email *models.Email, now time.Time) bool {
	if email.InternalDate.IsZero() {
		return true
	}

	cutoff := now.Add(-EmailValidityWindow)
	return !email.InternalDate.Before(cutoff) // inclusif
}
