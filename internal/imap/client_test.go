package imap

import (
	"bufio"
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
		tag := strings.Fields(command)[0]
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
