package imap

import (
	"fmt"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type StandardClient struct {
	client  *client.Client
	timeout time.Duration
}

// NewStandardClient creates a new StandardClient with a default timeout of 30 seconds for IMAP operations
func NewStandardClient() *StandardClient {
	return &StandardClient{
		timeout: 30 * time.Second,
	}
}

// Connect establishes a secure connection to the IMAP server using TLS. It returns an error if the connection fails.
func (c *StandardClient) Connect(server string) error {
	cl, err := client.DialTLS(server, nil)
	if err != nil {
		return fmt.Errorf("IMAP connection error: %w", err)
	}
	c.client = cl
	return nil
}

// Login authenticates the user with the IMAP server using the provided username and password. It returns an error if authentication fails or if there is no active connection.
func (c *StandardClient) Login(user, password string) error {
	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Login(user, password)
}

// SelectMailbox selects the specified mailbox (e.g., "INBOX") for subsequent operations. It returns an error if the mailbox cannot be selected or if there is no active connection.
func (c *StandardClient) SelectMailbox(name string) error {
	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	_, err := c.client.Select(name, false)
	return err
}

// ListUnseenUIDs retrieves the UIDs of unseen emails that have been received within the specified duration (e.g., last 15 minutes). It returns a slice of UIDs and an error if the search operation fails or if there is no active connection.
func (c *StandardClient) ListUnseenUIDs(since time.Duration) ([]uint32, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	criteria.Since = time.Now().Add(-since)

	uids, err := c.client.Search(criteria)
	if err != nil {
		return nil, fmt.Errorf("error searching for recent emails: %w", err)
	}

	return uids, nil
}

// FetchMessage retrieves the full email message corresponding to the specified UID. It returns an imap.Message struct containing the email data and an error if the fetch operation fails, if there is no active connection, or if no message is retrieved for the given UID.
func (c *StandardClient) FetchMessage(uid uint32) (*imap.Message, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem(), imap.FetchInternalDate, imap.FetchUid}

	prevTimeout := c.client.Timeout
	c.client.Timeout = c.timeout
	defer func() { c.client.Timeout = prevTimeout }()

	messages := make(chan *imap.Message, 1)
	done := make(chan error, 1)

	go func() {
		done <- c.client.Fetch(seqSet, items, messages)
	}()

	var msg *imap.Message
	for m := range messages {
		msg = m
	}

	if err := <-done; err != nil {
		return nil, fmt.Errorf("error fetching message UID %d: %w", uid, err)
	}

	if msg == nil {
		return nil, fmt.Errorf("no message retrieved for UID %d", uid)
	}

	return msg, nil
}

// MarkSeen marks the email with the specified UID as seen (read) on the IMAP server. It returns an error if the store operation fails or if there is no active connection.
func (c *StandardClient) MarkSeen(uid uint32) error {
	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)

	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.SeenFlag}

	return c.client.Store(seqSet, item, flags, nil)
}

// Close logs out from the IMAP server and closes the connection. It returns an error if the logout operation fails. If there is no active connection, it simply returns nil.
func (c *StandardClient) Close() error {
	if c.client == nil {
		return nil
	}
	return c.client.Logout()
}
