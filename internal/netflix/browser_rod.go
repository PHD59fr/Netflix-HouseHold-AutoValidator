package netflix

import (
	"context"
	"fmt"
	"net/url"
	"netflix-household-validator/internal/models"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"netflix-household-validator/internal/logging"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
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

type pageOutcome int

const (
	outcomeUnknown pageOutcome = iota
	outcomeConfirmed
	outcomeExpired
	outcomeLogin
)

// attemptOpenLink performs a single attempt to open the link and interact with the page.
func (rb *RodBrowser) attemptOpenLink(
	link string,
	attempt int,
	traceID string,
) (result models.BrowserResult, err error) {
	activeRodSessions.Add(1)
	defer activeRodSessions.Add(-1)

	locallog := logging.Log.WithField("trace_id", traceID)

	// Recover from any panic in the browser automation so a single bad link
	// can never crash the whole validator / IMAP loop.
	defer func() {
		if r := recover(); r != nil {
			locallog.Errorf("Recovered from panic in attemptOpenLink: %v", r)
			result = models.ResultFailed
			err = fmt.Errorf("panic in browser automation: %v", r)
		}
	}()

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

	const systemChromium = "/usr/bin/chromium"
	if _, err := os.Stat(systemChromium); err == nil {
		u = u.Bin(systemChromium)
	}

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

	// Create an empty page first. Creating the target with the URL up front
	// (browser.Page(URL)) requires the initial navigation to complete, which
	// fails with net::ERR_ABORTED as soon as Netflix redirects (login, expired
	// or already-consumed token). An empty page avoids that so we can navigate
	// ourselves and let racePageElements decide the real outcome below.
	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		locallog.WithError(err).Error("failed to create page")
		return models.ResultFailed, err
	}
	defer func() { _ = page.Close() }()

	// Navigate to the link. An aborted navigation (net::ERR_ABORTED) is not
	// fatal here: the page may still have been redirected to a confirm, login
	// or expired-token page. racePageElements below will tell which.
	if err := page.Navigate(link); err != nil {
		locallog.WithError(err).Warnf("Attempt %d: navigation aborted (redirect or invalid link), checking page state", attempt)
	}

	if err := page.WaitLoad(); err != nil {
		locallog.WithError(err).Warnf("Attempt %d: wait load failed (navigation may have been redirected)", attempt)
	}

	// Try to accept cookie banner if present
	if cookieBtn, err := page.Timeout(5 * time.Second).Element("#onetrust-accept-btn-handler"); err == nil {
		locallog.Info("Cookie banner detected, accepting")
		if clickErr := cookieBtn.Click(proto.InputMouseButtonLeft, 1); clickErr != nil {
			locallog.WithError(clickErr).Warn("Failed to click cookie banner")
		}
	}

	outcome, err := racePageElements(page, 30*time.Second)
	if err != nil {
		locallog.WithError(err).Warnf("Attempt %d: page race failed", attempt)
		return models.ResultFailed, err
	}

	switch outcome {
	case outcomeConfirmed:
		locallog.Info("Clicked on confirm button successfully")
		return models.ResultSuccess, nil

	case outcomeExpired:
		locallog.Info("Expired link detected (upl-invalid-token present)")
		return models.ResultExpired, nil

	case outcomeLogin:
		locallog.Info("Login required but credentials unavailable, aborting link")
		return models.ResultAbort, nil
	}

	locallog.Warnf("Attempt %d: timed out waiting for page elements", attempt)
	return models.ResultFailed, nil
}

// racePageElements races between confirm button and expired-token element.
// Returns the outcome.
func racePageElements(page *rod.Page, timeout time.Duration) (pageOutcome, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		state, err := page.Eval(`() => {
const visible = (element) => {
if (!element) {
return false;
}

const style = window.getComputedStyle(element);
const rect = element.getBoundingClientRect();

return style.display !== "none" &&
style.visibility !== "hidden" &&
rect.width > 0 &&
rect.height > 0;
};

const confirm = document.querySelector(
'[data-uia="set-primary-location-action"]'
);

const expired = document.querySelector(
'[data-uia="upl-invalid-token"]'
);

const login = document.querySelector(
'input[name="userLoginId"]'
);

return {
confirmExists: Boolean(confirm),
confirmVisible: visible(confirm),
expiredExists: Boolean(expired),
loginExists: Boolean(login)
};
}`)

		if err == nil && state != nil {
			var current struct {
				ConfirmExists  bool `json:"confirmExists"`
				ConfirmVisible bool `json:"confirmVisible"`
				ExpiredExists  bool `json:"expiredExists"`
				LoginExists    bool `json:"loginExists"`
			}

			if err := state.Value.Unmarshal(&current); err == nil {
				if current.ExpiredExists {
					return outcomeExpired, nil
				}

				if current.LoginExists {
					return outcomeLogin, nil
				}

				if current.ConfirmExists {
					logging.Log.Infof(
						"Netflix confirm button detected: visible=%t",
						current.ConfirmVisible,
					)

					clickResult, err := page.Eval(`() => {
const button = document.querySelector(
'[data-uia="set-primary-location-action"]'
);

if (!button) {
return {
ok: false,
error: "confirmation button not found"
};
}

button.scrollIntoView({
block: "center",
inline: "center"
});

button.click();

return {
ok: true,
text: (button.innerText || "").trim()
};
}`)

					if err != nil {
						return outcomeUnknown, fmt.Errorf(
							"JavaScript confirmation click failed: %w",
							err,
						)
					}

					if clickResult == nil {
						return outcomeUnknown, fmt.Errorf(
							"JavaScript confirmation click returned no result",
						)
					}

					logging.Log.Info(
						"Netflix confirmation button clicked via JavaScript",
					)

					return outcomeConfirmed, nil
				}
			}
		}

		time.Sleep(250 * time.Millisecond)
	}

	return outcomeUnknown, context.DeadlineExceeded
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
