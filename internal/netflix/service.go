package netflix

import (
	"netflix-household-validator/internal/models"
	"strings"

	"netflix-household-validator/internal/logging"
	"netflix-household-validator/internal/mailparse"
)

type Service struct {
	browser Browser
	config  *models.Config
}

// NewService creates a new instance of the Netflix Service with the provided browser and configuration
func NewService(browser Browser, cfg *models.Config) *Service {
	return &Service{
		browser: browser,
		config:  cfg,
	}
}

// HandleEmail processes the given email, applying filters and using the browser to handle valid emails.
func (s *Service) HandleEmail(email *models.Email) bool {
	locallog := logging.Log.WithField("trace_id", email.TraceID)

	// Filter by sender
	if email.From != s.config.TargetFrom {
		locallog.Infof("Email received from %s, skip ...", email.From)
		return false
	}

	// Filter by subject
	if email.Subject != s.config.TargetSubject {
		locallog.Infof("Email subject not recognized: %s", email.Subject)
		return false
	}

	// Check body not empty
	if email.BodyText == "" {
		locallog.Info("Empty email body, nothing to process")
		return false
	}

	// Extract and process links
	links := mailparse.ExtractLinks(email.BodyText)
	for _, link := range links {
		if !strings.Contains(link, "update-primary-location") {
			continue
		}

		// Determine credentials
		netflixEmail := ""
		netflixPassword := ""

		if s.config.FilterByAccount {
			found := false
			for _, account := range s.config.NetflixAuth {
				if account.Email == email.ToPrimary {
					locallog.Infof("Email received for %s", account.Email)
					netflixEmail = account.Email
					netflixPassword = account.Password
					found = true
					break
				}
			}
			if !found {
				locallog.Infof("No matching Netflix account found for To: %s", email.ToPrimary)
				return false
			}
		} else {
			locallog.Infof("Email received for %s", email.ToPrimary)
		}

		// Open link with browser
		result, err := s.browser.OpenUpdatePrimaryLocation(link, netflixEmail, netflixPassword, email.TraceID)
		if err != nil {
			locallog.WithError(err).Error("Browser error")
			return false
		}

		switch result {
		case models.ResultSuccess, models.ResultExpired:
			return true
		case models.ResultAbort:
			return false
		case models.ResultFailed:
			return false
		}
	}

	locallog.Info("No update-primary-location link found in email")
	return false
}
