package main

import (
	"io"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/google/uuid"
)

var failureSleepDuration = 30 * time.Minute
var imapFailureCount atomic.Int32

func fetchLastUnseenEmail(config Config) {
	c, err := client.DialTLS(config.Email.Imap, nil)
	if err != nil {
		failures := imapFailureCount.Add(1)
		log.Errorf("IMAP connection error: %v", err)

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

			log.Warnf("IMAP failed %d times, waiting %s before next attempt", failures, backoff)
			time.Sleep(backoff)
		}
		return
	}

	imapFailureCount.Store(0)

	defer func() {
		if err := c.Logout(); err != nil {
			log.Errorf("Logout error: %v", err)
		}
	}()

	if err := c.Login(config.Email.Login, config.Email.Password); err != nil {
		log.Errorf("Login error: %v", err)
		return
	}

	_, err = c.Select(config.Email.MailBox, false)
	if err != nil {
		log.Errorf("Folder selection error: %v", err)
		return
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	criteria.Since = time.Now().Add(-15 * time.Minute)

	uids, err := c.Search(criteria)
	if err != nil {
		log.Errorf("Error searching for recent emails: %v", err)
		return
	}

	if len(uids) == 0 {
		return
	}

	// Process all unseen emails
	for _, uid := range uids {
		if err := processEmail(c, uid, config); err != nil {
			log.Errorf("Error processing email UID %d: %v", uid, err)
		}
	}
}

func processEmail(c *client.Client, uid uint32, config Config) error {
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)

	traceID := uuid.New().String()
	locallog := log.WithField("trace_id", traceID)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem()}

	prevTimeout := c.Timeout
	c.Timeout = 30 * time.Second
	defer func() { c.Timeout = prevTimeout }()

	messages := make(chan *imap.Message, 1)
	done := make(chan error, 1)

	go func() {
		done <- c.Fetch(seqSet, items, messages)
	}()

	var msg *imap.Message
	for m := range messages {
		msg = m
	}

	if err := <-done; err != nil {
		locallog.Errorf("Error fetching message UID %d: %v", uid, err)
		return err
	}

	if msg == nil {
		locallog.Infof("No message retrieved for UID %d", uid)
		return nil
	}

	if !msg.InternalDate.IsZero() && time.Since(msg.InternalDate) > 15*time.Minute {
		locallog.Infof("Message UID %d is older than 15 minutes (date: %v), skipping", uid, msg.InternalDate)
		return nil
	}

	r := msg.GetBody(section)
	if r == nil {
		locallog.Errorf("Message body could not be retrieved for UID %d", uid)
		return nil
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		locallog.Errorf("Error creating mail reader for UID %d: %v", uid, err)
		return err
	}

	handled := handleEmail(mr, config, traceID)
	if handled {
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		flags := []interface{}{imap.SeenFlag}
		if err := c.Store(seqSet, item, flags, nil); err != nil {
			locallog.Errorf("Error marking message UID %d as seen: %v", uid, err)
		}
	}

	return nil
}

// Update signature to accept traceID
func handleEmail(mr *mail.Reader, config Config, traceID string) bool {
	locallog := log.WithField("trace_id", traceID)

	var emailBody, toEmail string

	header := mr.Header
	if toList, err := header.AddressList("To"); err == nil && len(toList) > 0 {
		toEmail = toList[0].Address
	}

	re := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	emailFrom := re.FindString(header.Get("From"))

	if emailFrom != config.TargetFrom {
		locallog.Infof("Email received from %s, skip ...", emailFrom)
		return false
	}

	decodedSubject, err := mimeDecoder(mr.Header.Get("Subject"))
	if err != nil {
		locallog.Errorf("Error decoding subject: %v", err)
		return false
	}

	if decodedSubject != config.TargetSubject {
		locallog.Infof("Email subject not recognized: %s", decodedSubject)
		return false
	}

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			locallog.Errorf("Error reading next message part: %v", err)
			return false
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			contentType, _, err := h.ContentType()
			if err != nil {
				locallog.Errorf("Error getting content type: %v", err)
				continue
			}
			if contentType == "text/plain" {
				body, err := io.ReadAll(p.Body)
				if err != nil {
					locallog.Errorf("Error reading body: %v", err)
					continue
				}
				emailBody = string(body)
			}
		}
	}

	if emailBody == "" {
		locallog.Info("Empty email body, nothing to process")
		return false
	}

	if config.FilterByAccount {
		for _, account := range config.NetflixAuth {
			if account.Email == toEmail {
				locallog.Infof("Email received for %s", account.Email)
				// pass traceID to browser flow
				return openLinkWithRod(emailBody, account.Email, account.Password, config, traceID)
			}
		}
		locallog.Infof("No matching Netflix account found for To: %s", toEmail)
		return false
	}

	locallog.Infof("Email received for %s", toEmail)
	return openLinkWithRod(emailBody, "", "", config, traceID)
}
