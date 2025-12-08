package main

import (
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/sirupsen/logrus"
)

type attemptResult int

const (
	attemptFailed attemptResult = iota
	attemptSuccess
	attemptExpired
	attemptAbort
)

func openLinkWithRod(body, netflixEmail, netflixPassword string, config Config) bool {
	links := extractLinks(body)

	for _, link := range links {
		if !strings.Contains(link, "update-primary-location") {
			continue
		}

		const maxAttempts = 3

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			logrus.Infof("Attempt %d/%d (fresh browser & profile)", attempt, maxAttempts)

			result := attemptOpenLink(
				link,
				netflixEmail,
				netflixPassword,
				config,
				attempt,
			)

			switch result {
			case attemptSuccess, attemptExpired:
				return true
			case attemptAbort:
				return false
			case attemptFailed:
				if attempt < maxAttempts {
					backoff := time.Duration(attempt) * time.Second
					logrus.Infof("Retrying in %s", backoff)
					time.Sleep(backoff)
				}
			}
		}

		logrus.Warn("All attempts failed, giving up on link")
		return false
	}

	logrus.Info("No update-primary-location link found in email")
	return false
}

func attemptOpenLink(
	link string,
	netflixEmail string,
	netflixPassword string,
	config Config,
	attempt int,
) attemptResult {

	tmpDir, err := os.MkdirTemp("", "rod-netflix-*")
	if err != nil {
		logrus.WithError(err).Error("failed to create temp user data dir")
		return attemptFailed
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			logrus.WithError(err).Warn("failed to remove temp user data dir")
		}
	}()

	u := launcher.New().
		Headless(true).
		NoSandbox(true).
		UserDataDir(tmpDir).
		MustLaunch()

	browser := rod.New().ControlURL(u).MustConnect()
	defer func() { _ = browser.Close() }()

	page := browser.MustPage(link)
	defer func() { _ = page.Close() }()

	page.MustWaitLoad()

	// Detect login form
	loginElement, err := page.Timeout(1 * time.Second).
		Element(`input[name='userLoginId']`)
	if err == nil {
		if config.FilterByAccount && netflixEmail != "" && netflixPassword != "" {
			logrus.Info("Login fields detected, attempting to log in")
			loginElement.MustInput(netflixEmail)
			page.MustElement(`input[name='password']`).MustInput(netflixPassword)
			page.MustElement(`[data-uia="login-submit-button"]`).MustClick()
			page.MustWaitLoad()
		} else {
			logrus.Info("Login required but credentials unavailable, aborting link")
			return attemptAbort
		}
	}

	// Check expired link
	element, err := page.Timeout(10 * time.Second).Element(
		`#appMountPoint > div > div > div > div.bd > div > div > div > div:nth-child(1) > h1`,
	)
	if err != nil {
		logrus.Warnf("Attempt %d: h1 not found after 10s", attempt)
		return attemptFailed
	}

	text, _ := element.Text()
	if strings.Contains(text, config.ExpiredLinkMessage) {
		logrus.Info("Link expired")
		return attemptExpired
	}

	// Confirm button
	confirmBtn, err := page.Timeout(10 * time.Second).
		Element(`[data-uia="set-primary-location-action"]`)
	if err != nil {
		logrus.Warn("Confirm button not found after 10s")
		return attemptFailed
	}

	confirmBtn.MustClick()
	logrus.Info("Clicked on 'Confirmer la mise à jour'")

	return attemptSuccess
}
