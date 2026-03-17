package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
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

// Connect establishes a secure connection to the IMAP server using TLS with TCP keepalive
// to prevent NAT/firewall from silently dropping idle connections.
func (c *StandardClient) Connect(server string) error {
	host, _, _ := net.SplitHostPort(server)
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	conn, err := tls.DialWithDialer(dialer, "tcp", server, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("IMAP connection error: %w", err)
	}
	cl, err := client.New(conn)
	if err != nil {
		_ = conn.Close()
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
	err := c.client.Logout()
	c.client = nil
	return err
}

// WaitForNewMail enters IMAP IDLE and blocks until the server signals a mailbox
// change or ctx is cancelled. IDLE is transparently re-issued every 25 minutes
// to prevent the server from closing the session (RFC 2177 recommends < 29 min).
func (c *StandardClient) WaitForNewMail(ctx context.Context) error {
	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	const idleRefreshInterval = 25 * time.Minute

	updates := make(chan client.Update, 8)
	c.client.Updates = updates
	defer func() { c.client.Updates = nil }()

	for {
		stop := make(chan struct{})
		idleDone := make(chan error, 1)
		go func() {
			idleDone <- c.client.Idle(stop, nil)
		}()

		refresh := time.NewTimer(idleRefreshInterval)
		reissue := false

		for !reissue {
			select {
			case <-ctx.Done():
				close(stop)
				<-idleDone
				refresh.Stop()
				return ctx.Err()
			case err := <-idleDone:
				refresh.Stop()
				if err != nil {
					return fmt.Errorf("IDLE terminated: %w", err)
				}
				return fmt.Errorf("IDLE terminated unexpectedly")
			case <-refresh.C:
				// Re-issue IDLE before the server closes the session
				close(stop)
				<-idleDone
				reissue = true
			case update := <-updates:
				if _, ok := update.(*client.MailboxUpdate); ok {
					close(stop)
					<-idleDone
					refresh.Stop()
					return nil
				}
			}
		}
	}
}
