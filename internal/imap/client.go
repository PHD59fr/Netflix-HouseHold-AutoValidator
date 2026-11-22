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

const (
	defaultCommandTimeout = 30 * time.Second
	logoutTimeout         = 5 * time.Second
	idleStopTimeout       = 10 * time.Second
	idlePollInterval      = 5 * time.Minute
	updatesBufferSize     = 64
)

type StandardClient struct {
	client         *client.Client
	conn           net.Conn
	timeout        time.Duration
	mailboxChanged chan struct{}
	updatesStop    chan struct{}
}

// NewStandardClient creates a new StandardClient with a default timeout of 30 seconds for IMAP operations
func NewStandardClient() *StandardClient {
	return &StandardClient{
		timeout: defaultCommandTimeout,
	}
}

// tlsConfig keeps TLS settings in one place so they can be tested and never
// silently fall back to an obsolete protocol version.
func tlsConfig(host string) *tls.Config {
	return &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}
}

// Connect establishes a secure connection to the IMAP server using TLS with TCP keepalive
// to prevent NAT/firewall from silently dropping idle connections.
func (c *StandardClient) Connect(ctx context.Context, server string) error {
	if c.client != nil {
		return fmt.Errorf("already connected")
	}

	host, _, err := net.SplitHostPort(server)
	if err != nil {
		return fmt.Errorf("invalid IMAP server address %q: %w", server, err)
	}

	dialer := &net.Dialer{
		Timeout:   defaultCommandTimeout,
		KeepAlive: 30 * time.Second,
	}
	tlsDialer := &tls.Dialer{
		NetDialer: dialer,
		Config:    tlsConfig(host),
	}
	conn, err := tlsDialer.DialContext(ctx, "tcp", server)
	if err != nil {
		return fmt.Errorf("IMAP connection error: %w", err)
	}

	// The IMAP greeting is read by client.New without any deadline of its own.
	if err := conn.SetDeadline(time.Now().Add(c.commandTimeout())); err != nil {
		_ = conn.Close()
		return fmt.Errorf("IMAP greeting deadline: %w", err)
	}
	cl, err := client.New(conn)
	clearErr := conn.SetDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("IMAP connection error: %w", err)
	}
	if clearErr != nil {
		_ = cl.Terminate()
		return fmt.Errorf("clear IMAP greeting deadline: %w", clearErr)
	}

	updates := make(chan client.Update, updatesBufferSize)
	mailboxChanged := make(chan struct{}, 1)
	updatesStop := make(chan struct{})

	// go-imap writes unsolicited updates to this channel from its reader
	// goroutine. Keep it assigned for the whole connection lifetime and drain
	// it continuously: leaving it nil loses updates, and leaving it full
	// blocks the reader (and therefore every command).
	cl.Updates = updates

	c.client = cl
	c.conn = conn
	c.mailboxChanged = mailboxChanged
	c.updatesStop = updatesStop

	go watchMailboxUpdates(updates, mailboxChanged, updatesStop)

	return nil
}

// watchMailboxUpdates drains go-imap updates and coalesces mailbox changes into
// a single signal. It must never block: the go-imap reader sends updates
// synchronously.
func watchMailboxUpdates(updates <-chan client.Update, mailboxChanged chan<- struct{}, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if _, ok := update.(*client.MailboxUpdate); !ok {
				continue
			}
			select {
			case mailboxChanged <- struct{}{}:
			default:
			}
		}
	}
}

// Login authenticates the user with the IMAP server using the provided username and password. It returns an error if authentication fails or if there is no active connection.
func (c *StandardClient) Login(ctx context.Context, user, password string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.withTimeout(c.commandTimeout(), func() error {
		return c.client.Login(user, password)
	})
}

// SelectMailbox selects the specified mailbox (e.g., "INBOX") for subsequent operations. It returns an error if the mailbox cannot be selected or if there is no active connection.
func (c *StandardClient) SelectMailbox(name string) error {
	return c.withTimeout(c.commandTimeout(), func() error {
		_, err := c.client.Select(name, false)
		return err
	})
}

// ListUnseenUIDs retrieves the UIDs of unseen emails that have been received within the specified duration (e.g., last 15 minutes). It returns a slice of UIDs and an error if the search operation fails or if there is no active connection.
func (c *StandardClient) ListUnseenUIDs(since time.Duration) ([]uint32, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	criteria.Since = imapSearchSince(time.Now(), since)

	var uids []uint32
	err := c.withTimeout(c.commandTimeout(), func() error {
		var searchErr error
		uids, searchErr = c.client.UidSearch(criteria)
		return searchErr
	})
	if err != nil {
		return nil, fmt.Errorf("error searching for recent emails: %w", err)
	}

	return uids, nil
}

// imapSearchSince builds a conservative UTC date for the IMAP SINCE criterion.
// SINCE only carries a day, not a time, and servers may interpret that date in
// their own timezone. Going one day back relative to the requested window in
// UTC guarantees a recent email is never excluded; each message is still
// checked against the precise validity window by the processor.
func imapSearchSince(now time.Time, since time.Duration) time.Time {
	date := now.UTC().Add(-since).AddDate(0, 0, -1)
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
}

// FetchMessage retrieves the full email message corresponding to the specified UID. It returns an imap.Message struct containing the email data and an error if the fetch operation fails, if there is no active connection, or if no message is retrieved for the given UID.
func (c *StandardClient) FetchMessage(uid uint32) (msg *imap.Message, err error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uid)

	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{section.FetchItem(), imap.FetchInternalDate, imap.FetchUid}

	err = c.withTimeout(c.commandTimeout(), func() error {
		messages := make(chan *imap.Message, 1)
		done := make(chan error, 1)

		go func() {
			done <- c.client.UidFetch(seqSet, items, messages)
		}()

		for m := range messages {
			msg = m
		}

		return <-done
	})
	if err != nil {
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

	err := c.withTimeout(c.commandTimeout(), func() error {
		return c.client.UidStore(seqSet, item, flags, nil)
	})
	if err != nil {
		return fmt.Errorf("error marking message UID %d as seen: %w", uid, err)
	}

	return nil
}

// Close logs out from the IMAP server and closes the connection. It returns an error if the logout operation fails. If there is no active connection, it simply returns nil.
func (c *StandardClient) Close() error {
	if c.client == nil {
		return nil
	}

	cl := c.client
	var err error
	if cl.State() != imap.LogoutState {
		err = c.withTimeout(logoutTimeout, func() error {
			return cl.Logout()
		})
	}
	if err != nil {
		// Never leave a half-open connection behind: the next Connect call
		// must start from a clean socket.
		_ = cl.Terminate()
	}

	if c.updatesStop != nil {
		close(c.updatesStop)
		c.updatesStop = nil
	}

	c.client = nil
	c.conn = nil
	c.mailboxChanged = nil
	return err
}

// WaitForNewMail enters IMAP IDLE and blocks until the server signals a mailbox
// change, the periodic safety poll fires, or ctx is cancelled.
//
// A periodic poll is used on top of IDLE so an email that arrived in a window
// where no IDLE session was active (for example during browser automation) is
// still processed.
func (c *StandardClient) WaitForNewMail(ctx context.Context) error {
	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	// A mailbox update may have arrived while the previous emails were being
	// processed. In that case, return immediately so the caller scans again
	// instead of entering IDLE with a pending change.
	select {
	case <-c.mailboxChanged:
		return nil
	default:
	}

	// Probe IDLE support with a normal command timeout. go-imap caches the
	// capability list, so the subsequent Idle call does not issue a command
	// that could block without a deadline.
	supportsIDLE, err := c.supportsIDLE()
	if err != nil {
		return fmt.Errorf("check IDLE support: %w", err)
	}
	if !supportsIDLE {
		return c.waitForPoll(ctx)
	}

	// IDLE is a long-lived command: it must not inherit a command deadline.
	c.client.Timeout = 0
	if c.conn != nil {
		if err := c.conn.SetDeadline(time.Time{}); err != nil {
			return fmt.Errorf("clear IMAP deadline before IDLE: %w", err)
		}
	}

	stop := make(chan struct{})
	idleDone := make(chan error, 1)
	go func() {
		idleDone <- c.client.Idle(stop, nil)
	}()

	poll := time.NewTimer(idlePollInterval)
	defer poll.Stop()

	select {
	case <-ctx.Done():
		// The connection may be half-open; do not wait forever for DONE.
		_ = c.stopIdle(stop, idleDone)
		return ctx.Err()
	case <-c.mailboxChanged:
		if err := c.stopIdle(stop, idleDone); err != nil {
			return fmt.Errorf("stop IDLE after new mail: %w", err)
		}
		return nil
	case err := <-idleDone:
		if err != nil {
			return fmt.Errorf("IDLE terminated: %w", err)
		}
		return fmt.Errorf("IDLE terminated unexpectedly")
	case <-poll.C:
		if err := c.stopIdle(stop, idleDone); err != nil {
			return fmt.Errorf("stop IDLE for periodic mailbox poll: %w", err)
		}
		return nil
	}
}

// supportsIDLE checks the server capability with a bounded command timeout.
func (c *StandardClient) supportsIDLE() (bool, error) {
	var supported bool
	err := c.withTimeout(c.commandTimeout(), func() error {
		var supportErr error
		supported, supportErr = c.client.Support("IDLE")
		return supportErr
	})
	return supported, err
}

// waitForPoll is used when the server does not support IDLE. It still returns
// periodically so the main loop re-scans the mailbox with bounded commands.
func (c *StandardClient) waitForPoll(ctx context.Context) error {
	timer := time.NewTimer(idlePollInterval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.mailboxChanged:
		return nil
	case <-timer.C:
		return nil
	}
}

// stopIdle asks go-imap to leave IDLE and forcibly terminates the connection if
// the server does not answer DONE in time.
func (c *StandardClient) stopIdle(stop chan struct{}, idleDone <-chan error) error {
	close(stop)

	select {
	case err := <-idleDone:
		return err
	case <-time.After(idleStopTimeout):
		if c.client != nil {
			_ = c.client.Terminate()
		}
		select {
		case <-idleDone:
		case <-time.After(idleStopTimeout):
		}
		return fmt.Errorf("IDLE did not stop within %s; connection terminated", idleStopTimeout)
	}
}

// withTimeout runs fn with a per-command deadline and clears the deadline
// afterwards. go-imap applies Client.Timeout with SetDeadline when a command
// starts; restoring the field alone is not enough because the reader keeps
// using the connection with the previous deadline.
func (c *StandardClient) withTimeout(timeout time.Duration, fn func() error) error {
	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	prev := c.client.Timeout
	c.client.Timeout = timeout
	defer func() {
		c.client.Timeout = prev
		if c.conn != nil {
			_ = c.conn.SetDeadline(time.Time{})
		}
	}()

	return fn()
}

func (c *StandardClient) commandTimeout() time.Duration {
	if c.timeout > 0 {
		return c.timeout
	}
	return defaultCommandTimeout
}
