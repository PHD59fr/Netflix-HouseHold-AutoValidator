package main

import (
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

type attemptResult int

const (
	attemptFailed attemptResult = iota
	attemptSuccess
	attemptExpired
	attemptAbort
)

func openLinkWithRod(body, netflixEmail, netflixPassword string, config Config, traceID string) bool {
	locallog := log.WithField("trace_id", traceID)

	links := extractLinks(body)

	for _, link := range links {
		if !strings.Contains(link, "update-primary-location") {
			continue
		}

		const maxAttempts = 3

		locallog.Info("Open page with rod : ", link)
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			locallog.Infof("Attempt %d/%d (fresh browser & profile)", attempt, maxAttempts)

			result := attemptOpenLink(
				link,
				netflixEmail,
				netflixPassword,
				config,
				attempt,
				traceID,
			)

			switch result {
			case attemptSuccess, attemptExpired:
				return true
			case attemptAbort:
				return false
			case attemptFailed:
				if attempt < maxAttempts {
					backoff := time.Duration(attempt) * time.Second
					locallog.Infof("Retrying in %s", backoff)
					time.Sleep(backoff)
				}
			}
		}

		locallog.Warn("All attempts failed, giving up on link")
		return false
	}

	locallog.Info("No update-primary-location link found in email")
	return false
}

func attemptOpenLink(
	link string,
	netflixEmail string,
	netflixPassword string,
	config Config,
	attempt int,
	traceID string,
) attemptResult {
	activeRodSessions.Add(1)
	defer activeRodSessions.Add(-1)

	locallog := log.WithField("trace_id", traceID)

	tmpDir, err := os.MkdirTemp("", "rod-netflix-*")
	if err != nil {
		locallog.WithError(err).Error("failed to create temp user data dir")
		return attemptFailed
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			locallog.WithError(err).Warn("failed to remove temp user data dir")
		}
	}()

	u := launcher.New().
		Headless(true). // set to false if you don't need to see the browser
		NoSandbox(true).
		UserDataDir(tmpDir).
		MustLaunch()

	browser := rod.New().ControlURL(u).MustConnect()
	defer func() { _ = browser.Close() }()

	page := browser.MustPage(link)
	defer func() { _ = page.Close() }()

	page.MustWaitLoad()

	// Try to accept cookie banner if present
	if cookieBtn, err := page.Timeout(5 * time.Second).Element("#onetrust-accept-btn-handler"); err == nil {
		locallog.Info("Cookie banner detected, accepting")
		cookieBtn.MustClick()
	}

	// Detect login form
	loginElement, err := page.Timeout(10 * time.Second).
		Element(`input[name='userLoginId']`)
	if err == nil {
		if config.FilterByAccount && netflixEmail != "" && netflixPassword != "" {
			locallog.Info("Login fields detected, attempting to log in")
			loginElement.MustInput(netflixEmail)
			page.MustElement(`input[name='password']`).MustInput(netflixPassword)
			page.MustElement(`[data-uia="login-submit-button"]`).MustClick()
			page.MustWaitLoad()

			// Cookie banner can reappear after login
			if cookieBtn, err := page.Timeout(5 * time.Second).Element("#onetrust-accept-btn-handler"); err == nil {
				locallog.Info("Cookie banner detected after login, accepting")
				cookieBtn.MustClick()
			}
		} else {
			locallog.Info("Login required but credentials unavailable, aborting link")
			return attemptAbort
		}
	}

	// Try to find the confirm button: if it exists, the link is valid
	confirmBtn, err := page.Timeout(10 * time.Second).
		Element(`[data-uia="set-primary-location-action"]`)
	if err == nil {
		confirmBtn.MustClick()
		locallog.Info("Clicked on confirm button successfully")
		return attemptSuccess
	}

	locallog.Warnf("Attempt %d: confirm button not found, checking for expired link message", attempt)

	// If confirm button is not found, check for the "invalid / expired" container
	_, err = page.Timeout(5 * time.Second).
		Element(`[data-uia="upl-invalid-token"]`)
	if err == nil {
		locallog.Info("Expired link detected (upl-invalid-token present)")
		return attemptExpired
	}

	locallog.Warnf("Attempt %d: confirm button not found and no expired message detected", attempt)
	return attemptFailed
}
