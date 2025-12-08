package main

import (
	"io"
	"regexp"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/sirupsen/logrus"
)

func fetchLastUnseenEmail(config Config) {
	c, err := client.DialTLS(config.Email.Imap, nil)
	if err != nil {
		logrus.Errorf("IMAP connection error: %v", err)
		return
	}
	defer func() {
		if err := c.Logout(); err != nil {
			logrus.Errorf("Logout error: %v", err)
		}
	}()

	if err := c.Login(config.Email.Login, config.Email.Password); err != nil {
		logrus.Errorf("Login error: %v", err)
		return
	}

	_, err = c.Select(config.Email.MailBox, false)
	if err != nil {
		logrus.Errorf("Folder selection error: %v", err)
		return
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}

	uids, err := c.Search(criteria)
	if err != nil {
		logrus.Errorf("Error searching for unseen emails: %v", err)
		return
	}

	if len(uids) == 0 {
		return
	}

	// Process all unseen emails
	for _, uid := range uids {
		if err := processEmail(c, uid, config); err != nil {
			logrus.Errorf("Error processing email UID %d: %v", uid, err)
		}
	}
}

func processEmail(c *client.Client, uid uint32, config Config) error {
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem()}
	messages := make(chan *imap.Message, 1)

	go func() {
		if err := c.Fetch(seqSet, items, messages); err != nil {
			logrus.Errorf("Error fetching message UID %d: %v", uid, err)
			close(messages)
		}
	}()

	msg, ok := <-messages
	if !ok || msg == nil {
		logrus.Infof("No message retrieved for UID %d", uid)
		return nil
	}

	r := msg.GetBody(section)
	if r == nil {
		logrus.Errorf("Message body could not be retrieved for UID %d", uid)
		return nil
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		logrus.Errorf("Error creating mail reader for UID %d: %v", uid, err)
		return err
	}

	handled := handleEmail(mr, config)

	if handled {
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		flags := []interface{}{imap.SeenFlag}
		if err := c.Store(seqSet, item, flags, nil); err != nil {
			logrus.Errorf("Error marking message UID %d as seen: %v", uid, err)
		}
	}

	return nil
}

func handleEmail(mr *mail.Reader, config Config) bool {
	var emailBody, toEmail string

	header := mr.Header
	if toList, err := header.AddressList("To"); err == nil && len(toList) > 0 {
		toEmail = toList[0].Address
	}

	re := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	emailFrom := re.FindString(header.Get("From"))

	if emailFrom != config.TargetFrom {
		logrus.Infof("Email received from %s, skip ...", emailFrom)
		return false
	}

	decodedSubject, err := mimeDecoder(mr.Header.Get("Subject"))
	if err != nil {
		logrus.Errorf("Error decoding subject: %v", err)
		return false
	}

	if decodedSubject != config.TargetSubject {
		logrus.Infof("Email subject not recognized: %s", decodedSubject)
		return false
	}

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			logrus.Errorf("Error reading next message part: %v", err)
			return false
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			contentType, _, err := h.ContentType()
			if err != nil {
				logrus.Errorf("Error getting content type: %v", err)
				continue
			}
			if contentType == "text/plain" {
				body, err := io.ReadAll(p.Body)
				if err != nil {
					logrus.Errorf("Error reading body: %v", err)
					continue
				}
				emailBody = string(body)
			}
		}
	}

	if emailBody == "" {
		logrus.Info("Empty email body, nothing to process")
		return false
	}

	if config.FilterByAccount {
		for _, account := range config.NetflixAuth {
			if account.Email == toEmail {
				logrus.Infof("Email received for %s", account.Email)
				return openLinkWithRod(emailBody, account.Email, account.Password, config)
			}
		}
		logrus.Infof("No matching Netflix account found for To: %s", toEmail)
		return false
	}

	logrus.Infof("Email received for %s", toEmail)
	return openLinkWithRod(emailBody, "", "", config)
}
