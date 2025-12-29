package main

import (
	"io"
	"regexp"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/google/uuid"
)

func fetchLastUnseenEmail(config Config) {
	c, err := client.DialTLS(config.Email.Imap, nil)
	if err != nil {
		log.Errorf("IMAP connection error: %v", err)
		return
	}
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

	// Generate trace id for this email processing flow
	traceID := uuid.New().String()
	locallog := log.WithField("trace_id", traceID)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem()}
	messages := make(chan *imap.Message, 1)

	go func() {
		if err := c.Fetch(seqSet, items, messages); err != nil {
			locallog.Errorf("Error fetching message UID %d: %v", uid, err)
			close(messages)
		}
	}()

	msg, ok := <-messages
	if !ok || msg == nil {
		locallog.Infof("No message retrieved for UID %d", uid)
		return nil
	}

	// Skip messages older than 15 minutes based on the server's internal date
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
		locallog.Infof("Message UID %d handled", uid)
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
