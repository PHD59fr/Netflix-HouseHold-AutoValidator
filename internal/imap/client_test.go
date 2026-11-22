package imap

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
)

type deadlineRecordingConn struct {
	net.Conn

	mu        sync.Mutex
	deadlines []time.Time
}

func (c *deadlineRecordingConn) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.deadlines = append(c.deadlines, deadline)
	c.mu.Unlock()
	return c.Conn.SetDeadline(deadline)
}

func (c *deadlineRecordingConn) recordedDeadlines() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	deadlines := make([]time.Time, len(c.deadlines))
	copy(deadlines, c.deadlines)
	return deadlines
}

func TestFetchMessageClearsTemporaryConnectionDeadline(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	recordedConn := &deadlineRecordingConn{Conn: clientConn}

	serverDone := make(chan error, 1)
	go func() {
		if _, err := fmt.Fprint(serverConn, "* OK [CAPABILITY IMAP4rev1] ready\r\n"); err != nil {
			serverDone <- err
			return
		}

		command, err := bufio.NewReader(serverConn).ReadString('\n')
		if err != nil {
			serverDone <- err
			return
		}
		fields := strings.Fields(command)
		if len(fields) < 3 || fields[1] != "UID" || fields[2] != "FETCH" {
			serverDone <- fmt.Errorf("expected UID FETCH command, got %q", strings.TrimSpace(command))
			return
		}
		tag := fields[0]
		_, err = fmt.Fprintf(
			serverConn,
			"* 42 FETCH (UID 42 INTERNALDATE \"02-Sep-2026 12:00:00 +0000\" BODY[] {4}\r\ntest)\r\n%s OK FETCH completed\r\n",
			tag,
		)
		serverDone <- err
	}()

	underlyingClient, err := imapclient.New(recordedConn)
	if err != nil {
		t.Fatalf("create IMAP client: %v", err)
	}
	defer underlyingClient.Terminate()
	underlyingClient.SetState(imap.SelectedState, nil)

	standardClient := &StandardClient{
		client:  underlyingClient,
		conn:    recordedConn,
		timeout: 30 * time.Second,
	}

	if _, err := standardClient.FetchMessage(42); err != nil {
		t.Fatalf("FetchMessage: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("mock IMAP server: %v", err)
	}

	deadlines := recordedConn.recordedDeadlines()
	if len(deadlines) < 2 {
		t.Fatalf("expected go-imap to set a deadline and FetchMessage to clear it, got %v", deadlines)
	}
	if deadlines[0].IsZero() {
		t.Fatalf("expected the temporary command deadline to be non-zero, got %v", deadlines[0])
	}
	if last := deadlines[len(deadlines)-1]; !last.IsZero() {
		t.Fatalf("temporary command deadline was left active: %v", last)
	}
}

func TestListUnseenUIDsUsesUidSearch(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	serverDone := make(chan error, 1)
	commands := make(chan string, 1)
	go func() {
		if _, err := fmt.Fprint(serverConn, "* OK [CAPABILITY IMAP4rev1] ready\r\n"); err != nil {
			serverDone <- err
			return
		}

		command, err := bufio.NewReader(serverConn).ReadString('\n')
		if err != nil {
			serverDone <- err
			return
		}

		commands <- command

		fields := strings.Fields(command)
		if len(fields) < 3 || fields[1] != "UID" || fields[2] != "SEARCH" {
			serverDone <- fmt.Errorf("expected UID SEARCH command, got %q", strings.TrimSpace(command))
			return
		}

		_, err = fmt.Fprintf(serverConn, "* SEARCH 42 43\r\n%s OK SEARCH completed\r\n", fields[0])
		serverDone <- err
	}()

	underlyingClient, err := imapclient.New(clientConn)
	if err != nil {
		t.Fatalf("create IMAP client: %v", err)
	}
	defer underlyingClient.Terminate()
	underlyingClient.SetState(imap.SelectedState, &imap.MailboxStatus{Name: "INBOX"})

	standardClient := &StandardClient{
		client:  underlyingClient,
		conn:    clientConn,
		timeout: 5 * time.Second,
	}

	before := time.Now()
	uids, err := standardClient.ListUnseenUIDs(15 * time.Minute)
	after := time.Now()
	if err != nil {
		t.Fatalf("ListUnseenUIDs: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("mock IMAP server: %v", err)
	}

	if len(uids) != 2 || uids[0] != 42 || uids[1] != 43 {
		t.Fatalf("expected UIDs [42 43], got %v", uids)
	}

	command := <-commands
	wantBefore := imapSearchSince(before, 15*time.Minute).Format("2-Jan-2006")
	wantAfter := imapSearchSince(after, 15*time.Minute).Format("2-Jan-2006")
	if !strings.Contains(command, wantBefore) && !strings.Contains(command, wantAfter) {
		t.Errorf("SEARCH command %q does not use the conservative UTC SINCE date (%q or %q)", command, wantBefore, wantAfter)
	}
}

func TestImapSearchSinceUsesPreviousUTCDay(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "midday UTC",
			now:  time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
			want: time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "just after midnight UTC",
			now:  time.Date(2026, 11, 22, 0, 5, 0, 0, time.UTC),
			want: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "non-UTC zone is converted first",
			now:  time.Date(2026, 11, 22, 0, 5, 0, 0, time.FixedZone("UTC+10", 10*60*60)),
			want: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := imapSearchSince(tt.now, 15*time.Minute)
			if !got.Equal(tt.want) {
				t.Errorf("imapSearchSince(%v) = %v, want %v", tt.now, got, tt.want)
			}
			if got.Location() != time.UTC {
				t.Errorf("imapSearchSince(%v) returned location %v, want UTC", tt.now, got.Location())
			}
		})
	}
}

func TestTLSConfigEnforcesMinimumVersion(t *testing.T) {
	cfg := tlsConfig("imap.example.com")

	if cfg.ServerName != "imap.example.com" {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, "imap.example.com")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS 1.2 (%x)", cfg.MinVersion, tls.VersionTLS12)
	}
}

func TestMarkSeenUsesUidStore(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	serverDone := make(chan error, 1)
	go func() {
		if _, err := fmt.Fprint(serverConn, "* OK [CAPABILITY IMAP4rev1] ready\r\n"); err != nil {
			serverDone <- err
			return
		}

		command, err := bufio.NewReader(serverConn).ReadString('\n')
		if err != nil {
			serverDone <- err
			return
		}

		fields := strings.Fields(command)
		if len(fields) < 3 || fields[1] != "UID" || fields[2] != "STORE" {
			serverDone <- fmt.Errorf("expected UID STORE command, got %q", strings.TrimSpace(command))
			return
		}

		_, err = fmt.Fprintf(serverConn, "%s OK STORE completed\r\n", fields[0])
		serverDone <- err
	}()

	underlyingClient, err := imapclient.New(clientConn)
	if err != nil {
		t.Fatalf("create IMAP client: %v", err)
	}
	defer underlyingClient.Terminate()
	underlyingClient.SetState(imap.SelectedState, &imap.MailboxStatus{Name: "INBOX"})

	standardClient := &StandardClient{
		client:  underlyingClient,
		conn:    clientConn,
		timeout: 5 * time.Second,
	}

	if err := standardClient.MarkSeen(42); err != nil {
		t.Fatalf("MarkSeen: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("mock IMAP server: %v", err)
	}
}

func TestWaitForNewMailReturnsOnMailboxUpdate(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	serverDone := make(chan error, 1)
	go func() {
		if _, err := fmt.Fprint(serverConn, "* OK [CAPABILITY IMAP4rev1 IDLE] ready\r\n"); err != nil {
			serverDone <- err
			return
		}

		reader := bufio.NewReader(serverConn)
		command, err := reader.ReadString('\n')
		if err != nil {
			serverDone <- err
			return
		}

		fields := strings.Fields(command)
		if len(fields) < 2 || fields[1] != "IDLE" {
			serverDone <- fmt.Errorf("expected IDLE command, got %q", strings.TrimSpace(command))
			return
		}

		if _, err := fmt.Fprint(serverConn, "+ idling\r\n"); err != nil {
			serverDone <- err
			return
		}

		// Simulate a new message arriving while IDLE is active.
		if _, err := fmt.Fprint(serverConn, "* 2 EXISTS\r\n"); err != nil {
			serverDone <- err
			return
		}

		done, err := reader.ReadString('\n')
		if err != nil {
			serverDone <- err
			return
		}
		if strings.TrimSpace(done) != "DONE" {
			serverDone <- fmt.Errorf("expected DONE, got %q", strings.TrimSpace(done))
			return
		}

		_, err = fmt.Fprintf(serverConn, "%s OK IDLE terminated\r\n", fields[0])
		serverDone <- err
	}()

	underlyingClient, err := imapclient.New(clientConn)
	if err != nil {
		t.Fatalf("create IMAP client: %v", err)
	}
	defer underlyingClient.Terminate()
	underlyingClient.SetState(imap.SelectedState, &imap.MailboxStatus{
		Name:  "INBOX",
		Items: make(map[imap.StatusItem]interface{}),
	})

	updates := make(chan imapclient.Update, 8)
	mailboxChanged := make(chan struct{}, 1)
	updatesStop := make(chan struct{})
	defer close(updatesStop)

	underlyingClient.Updates = updates
	go watchMailboxUpdates(updates, mailboxChanged, updatesStop)

	standardClient := &StandardClient{
		client:         underlyingClient,
		conn:           clientConn,
		timeout:        5 * time.Second,
		mailboxChanged: mailboxChanged,
		updatesStop:    updatesStop,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := standardClient.WaitForNewMail(ctx); err != nil {
		t.Fatalf("WaitForNewMail: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("mock IMAP server: %v", err)
	}
}
