package main

import (
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/sirupsen/logrus"
	"io"
	"regexp"
)

func fetchLastUnseenEmail(config Config) {
	c, err := client.DialTLS(config.Email.Imap, nil)
	if err != nil {
		logrus.Fatalf("IMAP connection error: %v", err)
	}
	defer func(c *client.Client) {
		err := c.Logout()
		if err != nil {
			logrus.Fatalf("Logout error: %v", err)
		}
	}(c)

	if err := c.Login(config.Email.Login, config.Email.Password); err != nil {
		logrus.Fatalf("Login error: %v", err)
	}

	_, err = c.Select(config.Email.MailBox, false)
	if err != nil {
		logrus.Fatalf("Folder selection error: %v", err)
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}

	uids, err := c.Search(criteria)
	if err != nil {
		logrus.Fatalf("Error searching for unseen emails: %v", err)
	}

	if len(uids) > 0 {
		processUnseenEmail(c, uids, config)
	}
}

func processUnseenEmail(c *client.Client, uids []uint32, config Config) {
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uids[len(uids)-1]) // Last unread message ID

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem()}
	messages := make(chan *imap.Message, 1)
	go func() {
		if err := c.Fetch(seqSet, items, messages); err != nil {
			logrus.Fatal(err)
		}
	}()

	msg := <-messages
	if msg == nil {
		logrus.Println("No message retrieved")
		return
	}

	r := msg.GetBody(section)
	if r == nil {
		logrus.Fatal("Message body could not be retrieved")
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		logrus.Fatalf("Error creating mail reader: %v", err)
	}

	handleEmail(mr, config)
}

func handleEmail(mr *mail.Reader, config Config) {
	var emailBody, toEmail string

	header := mr.Header
	if toList, err := header.AddressList("To"); err == nil && len(toList) > 0 {
		toEmail = toList[0].Address
	}

	re := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	emailFrom := re.FindString(header.Get("From"))

	if emailFrom != config.TargetFrom {
		logrus.Infof("Email received from %s skip ...", emailFrom)
		return
	}

	decodedSubject, err := mimeDecoder(mr.Header.Get("Subject"))
	if err != nil {
		logrus.Errorf("Error decoding subject: %v", err)
		return
	}

	if decodedSubject != config.TargetSubject {
		logrus.Infof("Email subject not recognized: %s", decodedSubject)
		return
	}

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			logrus.Fatal(err)
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			contentType, _, err := h.ContentType()
			if err != nil {
				logrus.Fatalf("Error getting content type: %v", err)
			}
			if contentType == "text/plain" {
				body, err := io.ReadAll(p.Body)
				if err != nil {
					logrus.Fatalf("Error reading body: %v", err)
				}
				emailBody = string(body)
			}
		}
	}

	if config.FilterByAccount {
		for _, account := range config.NetflixAuth {
			if account.Email == toEmail {
				logrus.Infof("Email received for %s", account.Email)
				openLinkWithRod(emailBody, account.Email, account.Password, config)
				break
			}
		}
	} else {
		logrus.Infof("Email received for %s", toEmail)
		openLinkWithRod(emailBody, "", "", config)
	}
}
