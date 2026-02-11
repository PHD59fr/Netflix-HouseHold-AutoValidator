package mailparse

import (
	"io"
	"mime"
	"regexp"

	"netflix-household-validator/internal/models"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-message/mail"
	"github.com/google/uuid"
)

func Parse(msg *imap.Message) (*models.Email, error) {
	section := &imap.BodySectionName{}
	r := msg.GetBody(section)
	if r == nil {
		return nil, io.EOF
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		return nil, err
	}

	email := &models.Email{
		UID:          msg.SeqNum,
		InternalDate: msg.InternalDate,
		TraceID:      uuid.New().String(),
	}

	header := mr.Header

	// Extract From
	email.From = extractEmailAddress(header.Get("From"))

	// Extract To (all addresses)
	if toList, err := header.AddressList("To"); err == nil {
		for _, addr := range toList {
			email.To = append(email.To, addr.Address)
		}
		if len(toList) > 0 {
			email.ToPrimary = toList[0].Address
		}
	}

	// Decode Subject
	decodedSubject, err := DecodeHeader(header.Get("Subject"))
	if err != nil {
		return nil, err
	}
	email.Subject = decodedSubject

	// Extract body text/plain
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			contentType, _, err := h.ContentType()
			if err != nil {
				continue
			}
			if contentType == "text/plain" {
				body, err := io.ReadAll(p.Body)
				if err != nil {
					continue
				}
				email.BodyText = string(body)
			}
		}
	}

	return email, nil
}

// Simple regex to extract email address from "From" header, which may contain name and email
func extractEmailAddress(fromHeader string) string {
	re := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	return re.FindString(fromHeader)
}

// DecodeHeader decodes MIME-encoded headers (e.g., "=?UTF-8?B?...?=") to plain text
func DecodeHeader(encoded string) (string, error) {
	decoder := new(mime.WordDecoder)
	decoded, err := decoder.DecodeHeader(encoded)
	if err != nil {
		return "", err
	}
	return decoded, nil
}

// ExtractLinks uses a regex to find all URLs in the given text
func ExtractLinks(text string) []string {
	re := regexp.MustCompile(`https?://[^\s"'<>)\]]+`)
	return re.FindAllString(text, -1)
}
