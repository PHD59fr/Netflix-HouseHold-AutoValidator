package imap

import (
	"context"
	"time"

	"github.com/emersion/go-imap"
)

type Client interface {
	Connect(ctx context.Context, server string) error
	Login(ctx context.Context, user, password string) error
	SelectMailbox(name string) error
	ListUnseenUIDs(since time.Duration) ([]uint32, error)
	FetchMessage(uid uint32) (*imap.Message, error)
	MarkSeen(uid uint32) error
	Close() error
	// WaitForNewMail blocks until the server signals new mail in the selected
	// mailbox (via IMAP IDLE), the periodic safety poll fires, or ctx is
	// cancelled.
	WaitForNewMail(ctx context.Context) error
}
