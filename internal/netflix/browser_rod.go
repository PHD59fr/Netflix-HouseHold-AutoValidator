package netflix

import (
	"net/url"
	"netflix-household-validator/internal/models"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"netflix-household-validator/internal/logging"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

var activeRodSessions atomic.Int32

type RodBrowser struct{}

// NewRodBrowser creates a new instance of RodBrowser
func NewRodBrowser() *RodBrowser {
	return &RodBrowser{}
}

// OpenUpdatePrimaryLocation attempts to open the provided link using Rod, handling login if necessary.
func (rb *RodBrowser) OpenUpdatePrimaryLocation(link, traceID string) (models.BrowserResult, error) {
	const maxAttempts = 3

	sanitizedLink := sanitizeURL(link)
	logging.Log.WithField("trace_id", traceID).Info("Open page with rod: ", sanitizedLink)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		logging.Log.WithField("trace_id", traceID).Infof("Attempt %d/%d (fresh browser & profile)", attempt, maxAttempts)

		result, err := rb.attemptOpenLink(link, attempt, traceID)
		if err != nil {
			logging.Log.WithField("trace_id", traceID).WithError(err).Warnf("Attempt %d error", attempt)
		}

		switch result {
		case models.ResultSuccess, models.ResultExpired:
			return result, nil
		case models.ResultAbort:
			return result, nil
		case models.ResultFailed:
			if attempt < maxAttempts {
				backoff := time.Duration(attempt) * time.Second
				logging.Log.WithField("trace_id", traceID).Infof("Retrying in %s", backoff)
				time.Sleep(backoff)
			}
		}
	}

	logging.Log.WithField("trace_id", traceID).Warn("All attempts failed, giving up on link")
	return models.ResultFailed, nil
}

// attemptOpenLink performs a single attempt to open the link and interact with the page.
func (rb *RodBrowser) attemptOpenLink(
	link string,
	attempt int,
	traceID string,
) (models.BrowserResult, error) {
	activeRodSessions.Add(1)
	defer activeRodSessions.Add(-1)

	locallog := logging.Log.WithField("trace_id", traceID)

	tmpDir, err := os.MkdirTemp("", "rod-netflix-*")
	if err != nil {
		locallog.WithError(err).Error("failed to create temp user data dir")
		return models.ResultFailed, err
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			locallog.WithError(err).Warn("failed to remove temp user data dir")
		}
	}()

	u := launcher.New().
		Headless(true).
		NoSandbox(true).
		UserDataDir(tmpDir)

	launchURL, err := u.Launch()
	if err != nil {
		locallog.WithError(err).Error("failed to launch browser")
		return models.ResultFailed, err
	}
	defer u.Cleanup()

	browser := rod.New()
	defer func() { _ = browser.Close() }()
	if err := browser.ControlURL(launchURL).Connect(); err != nil {
		locallog.WithError(err).Error("failed to connect to browser")
		return models.ResultFailed, err
	}

	page := browser.MustPage(link)
	defer func() { _ = page.Close() }()

	page.MustWaitLoad()

	// Try to accept cookie banner if present
	if cookieBtn, err := page.Timeout(5 * time.Second).Element("#onetrust-accept-btn-handler"); err == nil {
		locallog.Info("Cookie banner detected, accepting")
		cookieBtn.MustClick()
	}

	// Detect login form
	_, err = page.Timeout(10 * time.Second).
		Element(`input[name='userLoginId']`)
	if err == nil {
		locallog.Info("Login required but credentials unavailable, aborting link")
		return models.ResultAbort, nil
	}

	// Try to find the confirm button: if it exists, the link is valid
	confirmBtn, err := page.Timeout(10 * time.Second).
		Element(`[data-uia="set-primary-location-action"]`)
	if err == nil {
		confirmBtn.MustClick()
		locallog.Info("Clicked on confirm button successfully")
		return models.ResultSuccess, nil
	}

	locallog.Warnf("Attempt %d: confirm button not found, checking for expired link message", attempt)

	// If confirm button is not found, check for the "invalid / expired" container
	_, err = page.Timeout(5 * time.Second).
		Element(`[data-uia="upl-invalid-token"]`)
	if err == nil {
		locallog.Info("Expired link detected (upl-invalid-token present)")
		return models.ResultExpired, nil
	}

	locallog.Warnf("Attempt %d: confirm button not found and no expired message detected", attempt)
	return models.ResultFailed, nil
}

// StartCleanup starts a background goroutine that cleans up old Rod temp directories
func StartCleanup() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			if activeRodSessions.Load() > 0 {
				logging.Log.Info("Skipping /tmp cleanup: active Rod sessions detected")
				continue
			}

			pattern := filepath.Join(os.TempDir(), "rod-netflix-*")
			matches, err := filepath.Glob(pattern)
			if err != nil {
				logging.Log.WithError(err).Warn("Failed to glob temp directories")
				continue
			}

			for _, dir := range matches {
				if err := os.RemoveAll(dir); err != nil {
					logging.Log.WithError(err).Warnf("Failed to remove temp dir: %s", dir)
				} else {
					logging.Log.Infof("Cleaned up temp dir: %s", dir)
				}
			}
		}
	}()
}

// sanitizeURL redacts sensitive query parameters from the URL for safe logging
func sanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	q := u.Query()

	redactKeys := map[string]struct{}{
		"nftoken": {},
		"g":       {},
	}

	for key := range redactKeys {
		if q.Has(key) {
			q.Set(key, "******")
		}
	}

	u.RawQuery = q.Encode()
	u.Fragment = ""

	return u.String()
}
