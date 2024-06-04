package main

import (
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/sirupsen/logrus"
)

func openLinkWithRod(body, netflixEmail, netflixPassword string, config Config) {
	links := extractLinks(body)
	for _, link := range links {
		if strings.Contains(link, "update-primary-location") {
			logrus.Infof("Opening link %s", strings.ReplaceAll(link, "]", ""))

			u := launcher.New().Headless(true).NoSandbox(true).MustLaunch() // set headless to false for debugging
			browser := rod.New().ControlURL(u).MustConnect()

			page := browser.MustPage(link)
			page.MustWaitLoad()

			loginElement, err := page.Timeout(10 * time.Second).Element(`input[name='userLoginId']`)
			if err == nil {
				if config.FilterByAccount && netflixEmail != "" && netflixPassword != "" {
					logrus.Info("Login fields detected, attempting to log in...")
					loginElement.MustInput(netflixEmail)
					page.MustElement(`input[name='password']`).MustInput(netflixPassword)
					page.MustElement(`[data-uia="login-submit-button"]`).MustClick()
					page.MustWaitLoad()
				} else {
					logrus.Info("Login fields detected, but filter account is disabled")
					page.MustClose()
					browser.MustClose()
					break
				}
			}

			element, err := page.Timeout(10 * time.Second).Element(`#appMountPoint > div > div > div > div.bd > div > div > div > div:nth-child(1) > h1`)
			if err == nil {
				text, _ := element.Text()
				if strings.Contains(text, config.ExpiredLinkMessage) {
					logrus.Info("Link expired")
				}
			}

			page.MustWaitLoad()
			page.MustElement(`[data-uia="set-primary-location-action"]`).MustClick()

			logrus.Info("Verification email end page opened")

			page.MustClose()
			browser.MustClose() // Close browser to clear cache and cookies

			logrus.Info("Cache and cookies cleared, closing page")
			break
		}
	}
}
