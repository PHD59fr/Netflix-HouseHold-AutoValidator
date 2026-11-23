package netflix

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"netflix-household-validator/internal/models"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"netflix-household-validator/internal/logging"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const (
	maxOpenAttempts       = 3
	attemptTimeout        = 3 * time.Minute
	navigationTimeout     = 30 * time.Second
	pageLoadTimeout       = 45 * time.Second
	cookieTimeout         = 5 * time.Second
	evaluationTimeout     = 5 * time.Second
	confirmationTimeout   = 15 * time.Second
	confirmButtonSelector = `[data-uia="update-household-confirm-cta"], [data-uia="set-primary-location-action"]`
)

var activeRodSessions atomic.Int32

type RodBrowser struct{}

// NewRodBrowser creates a new instance of RodBrowser
func NewRodBrowser() *RodBrowser {
	return &RodBrowser{}
}

// OpenUpdatePrimaryLocation attempts to open the provided link using Rod, handling login if necessary.
func (rb *RodBrowser) OpenUpdatePrimaryLocation(ctx context.Context, link, traceID string) (models.BrowserResult, error) {
	logging.Log.WithField("trace_id", traceID).Info("Opening Netflix confirmation page")

	for attempt := 1; attempt <= maxOpenAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return models.ResultFailed, err
		}

		logging.Log.WithField("trace_id", traceID).Infof("Attempt %d/%d (fresh browser & profile)", attempt, maxOpenAttempts)

		result, err := rb.attemptOpenLink(ctx, link, attempt, traceID)
		if err != nil {
			logging.Log.WithField("trace_id", traceID).WithError(err).Warnf("Attempt %d error", attempt)
		}

		switch result {
		case models.ResultSuccess, models.ResultExpired:
			return result, nil
		case models.ResultAbort:
			return result, nil
		case models.ResultFailed:
			if attempt < maxOpenAttempts {
				backoff := time.Duration(attempt) * time.Second
				logging.Log.WithField("trace_id", traceID).Infof("Retrying in %s", backoff)
				select {
				case <-ctx.Done():
					return models.ResultFailed, ctx.Err()
				case <-time.After(backoff):
				}
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
	ctx context.Context,
	link string,
	attempt int,
	traceID string,
) (result models.BrowserResult, err error) {
	activeRodSessions.Add(1)
	defer activeRodSessions.Add(-1)

	locallog := logging.Log.WithField("trace_id", traceID)

	// Bound the whole attempt (launch, navigation, confirmation) so a stuck
	// browser can never block the IMAP loop forever.
	ctx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()

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
		UserDataDir(tmpDir).
		Context(ctx)

	const systemChromium = "/usr/bin/chromium"
	if _, err := os.Stat(systemChromium); err == nil {
		u = u.Bin(systemChromium)
	}

	launchURL, err := u.Launch()
	if err != nil {
		locallog.WithError(err).Error("failed to launch browser")
		return models.ResultFailed, err
	}
	defer cleanupLauncher(u)

	browser := rod.New().Context(ctx)
	defer func() {
		if closeErr := browser.Close(); closeErr != nil {
			locallog.WithError(closeErr).Warn("failed to close browser")
			// Make sure the launcher's Cleanup cannot wait forever for a
			// browser that did not shut down cleanly.
			u.Kill()
		}
	}()
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
	page = page.Context(ctx)
	defer func() { _ = page.Close() }()

	// Navigate to the link. An aborted navigation (net::ERR_ABORTED) is not
	// fatal here: the page may still have been redirected to a confirm, login
	// or expired-token page. racePageElements below will tell which.
	if err := page.Timeout(navigationTimeout).Navigate(link); err != nil {
		locallog.WithError(err).Warnf("Attempt %d: navigation aborted (redirect or invalid link), checking page state", attempt)
	}

	if err := page.Timeout(pageLoadTimeout).WaitLoad(); err != nil {
		locallog.WithError(err).Warnf("Attempt %d: wait load failed (navigation may have been redirected)", attempt)
	}

	// Try to accept cookie banner if present
	if cookieBtn, err := page.Timeout(cookieTimeout).Element("#onetrust-accept-btn-handler"); err == nil {
		locallog.Info("Cookie banner detected, accepting")
		if clickErr := cookieBtn.Click(proto.InputMouseButtonLeft, 1); clickErr != nil {
			locallog.WithError(clickErr).Warn("Failed to click cookie banner")
		}
	}

	outcome, err := racePageElements(page, 30*time.Second, traceID)
	if err != nil {
		locallog.WithError(err).Warnf("Attempt %d: page race failed", attempt)
		return models.ResultFailed, err
	}

	switch outcome {
	case outcomeConfirmed:
		locallog.Info("Netflix household confirmation verified")
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

type pageState struct {
	ConfirmExists  bool   `json:"confirmExists"`
	ConfirmVisible bool   `json:"confirmVisible"`
	ConfirmEnabled bool   `json:"confirmEnabled"`
	ExpiredExists  bool   `json:"expiredExists"`
	LoginExists    bool   `json:"loginExists"`
	SuccessVisible bool   `json:"successVisible"`
	BodyText       string `json:"bodyText"`
	URL            string `json:"url"`
}

// readPageState inspects the page without clicking anything. It only reports
// the confirm button as clickable when it is present, visible and enabled.
func readPageState(page *rod.Page) (*pageState, error) {
	ctx, cancel := context.WithTimeout(page.GetContext(), evaluationTimeout)
	defer cancel()

	state, err := page.Context(ctx).Eval(`() => {
const rendered = (element) => {
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

const visible = (element) => rendered(element) &&
window.getComputedStyle(element).pointerEvents !== "none";

const enabled = (element) => {
if (!element) {
return false;
}

return !element.disabled &&
element.getAttribute("aria-disabled") !== "true" &&
!element.classList.contains("disabled");
};

const confirm = document.querySelector(
'[data-uia="update-household-confirm-cta"], [data-uia="set-primary-location-action"]'
);

return {
confirmExists: Boolean(confirm),
confirmVisible: visible(confirm),
confirmEnabled: enabled(confirm),
expiredExists: Boolean(document.querySelector('[data-uia="upl-invalid-token"]')),
loginExists: Boolean(document.querySelector('input[name="userLoginId"]')),
successVisible: rendered(document.querySelector(
'[data-uia="update-household-success"], [data-uia="set-primary-location-success"], [data-uia="upl-success"]'
)),
bodyText: document.body ? document.body.innerText : "",
url: location.href
};
}`)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("empty page state")
	}

	var current pageState
	if err := state.Value.Unmarshal(&current); err != nil {
		return nil, err
	}

	return &current, nil
}

// Recheck the live target in the same JS task as activation. A navigation
// can destroy the execution context before Chromium acknowledges the call.
func clickConfirmButton(page *rod.Page) (bool, string) {
	ctx, cancel := context.WithTimeout(page.GetContext(), evaluationTimeout)
	defer cancel()
	button, err := page.Context(ctx).Element(confirmButtonSelector)
	if err != nil {
		return false, "target lookup failed"
	}
	// Binding the evaluation to this element also prevents Rod from replaying
	// the activation in a new document if the execution context disappears.
	res, err := button.Eval(`() => {
        const button = this;
        if (!button.isConnected) return "detached";
        if (button.matches(":disabled, [aria-disabled=true], .disabled")) return "disabled";
        button.scrollIntoView({block: "center", inline: "nearest", behavior: "instant"});
        const rect = button.getBoundingClientRect();
        const hit = document.elementFromPoint(rect.left + rect.width/2, rect.top + rect.height/2);
        if (!hit || !button.contains(hit)) return "covered or outside viewport";
        button.click();
        return "dispatched";
    }`)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return true, "evaluation timed out; delivery unknown"
		}
		return true, "evaluation interrupted; delivery unknown"
	}
	status := res.Value.Str()
	return status == "dispatched", status
}

// racePageElements waits for the confirm button, clicks it only when it is
// visible and enabled, then waits for a real success signal before reporting
// outcomeConfirmed. A click that does nothing is treated as a failure.
func racePageElements(page *rod.Page, timeout time.Duration, traceID string) (pageOutcome, error) {
	locallog := logging.Log.WithField("trace_id", traceID)
	deadline := time.Now().Add(timeout)

	awaitingConfirmation := false
	var clickedAt time.Time

	for {
		if ctxErr := page.GetContext().Err(); ctxErr != nil {
			return outcomeUnknown, ctxErr
		}
		if !awaitingConfirmation && time.Now().After(deadline) {
			return outcomeUnknown, context.DeadlineExceeded
		}
		if awaitingConfirmation && time.Since(clickedAt) > confirmationTimeout {
			return outcomeUnknown, fmt.Errorf("no explicit Netflix confirmation within %s of click", confirmationTimeout)
		}

		state, err := readPageState(page)
		if err != nil {
			locallog.Debugf("Netflix page state unavailable (%T)", err)
			time.Sleep(250 * time.Millisecond)
			continue
		}

		if state.ExpiredExists ||
			strings.Contains(state.URL, "upl-invalid-token") ||
			strings.Contains(strings.ToLower(state.URL), "expired") {
			return outcomeExpired, nil
		}

		if state.LoginExists || isLoginURL(state.URL) {
			return outcomeLogin, nil
		}

		if !awaitingConfirmation {
			if state.ConfirmExists && state.ConfirmVisible && state.ConfirmEnabled {
				locallog.Infof(
					"Netflix confirm button detected: visible=%t enabled=%t",
					state.ConfirmVisible,
					state.ConfirmEnabled,
				)

				started := time.Now()
				attempted, status := clickConfirmButton(page)
				if status != "dispatched" {
					locallog.Warnf("Netflix confirmation click: %s (elapsed %s)", status, time.Since(started).Round(time.Millisecond))
				}
				if attempted {
					awaitingConfirmation = true
					clickedAt = time.Now()
				}
			}

			time.Sleep(250 * time.Millisecond)
			continue
		}

		// Only an explicit confirmation is enough to mark the email as seen.
		// A redirect or a vanished button can also mean an application error.
		if isConfirmationSuccess(state) {
			return outcomeConfirmed, nil
		}

		time.Sleep(250 * time.Millisecond)
	}
}

func isConfirmationSuccess(state *pageState) bool {
	if state.SuccessVisible {
		return true
	}

	text := strings.ToLower(strings.Join(strings.Fields(state.BodyText), " "))
	for _, phrase := range []string{
		"your netflix household has been updated",
		"netflix household updated",
		"votre foyer netflix a été mis à jour",
		"votre foyer netflix est à jour",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func isLoginURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}

	path := strings.ToLower(strings.TrimSuffix(u.Path, "/"))
	return path == "/login" || strings.HasPrefix(path, "/login/")
}

// cleanupLauncher waits for the browser process to exit and removes its
// profile, but never blocks the IMAP loop forever if Chromium hangs.
func cleanupLauncher(u *launcher.Launcher) {
	done := make(chan struct{})
	go func() {
		u.Cleanup()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		u.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
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

const redactedValue = "******"

// sensitiveQueryKeyPatterns matches query parameter names whose values must
// never be logged (tokens, credentials, identifiers and personal data).
var sensitiveQueryKeyPatterns = []string{
	"token", "code", "secret", "password", "passwd", "pwd",
	"auth", "session", "sid", "sig", "key", "otp", "email", "mail",
}

// sanitizeURL redacts sensitive query parameters, embedded credentials and
// fragments from the URL before it is written to the logs.
func sanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		// An unparsable URL cannot be proven safe: hide everything that could
		// contain a secret instead of logging it verbatim.
		if idx := strings.Index(raw, "?"); idx >= 0 {
			return raw[:idx] + "?" + redactedValue
		}
		return redactedValue
	}

	u.User = nil
	u.Fragment = ""

	q := u.Query()
	for key := range q {
		if isSensitiveQueryKey(key) {
			q.Set(key, redactedValue)
		}
	}
	u.RawQuery = q.Encode()

	return u.String()
}

func isSensitiveQueryKey(key string) bool {
	lower := strings.ToLower(key)
	if lower == "g" {
		return true
	}

	for _, pattern := range sensitiveQueryKeyPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}
