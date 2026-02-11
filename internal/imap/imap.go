package imap

import (
	"time"

	"github.com/emersion/go-imap"
)

type Client interface {
	Connect(server string) error
	Login(user, password string) error
	SelectMailbox(name string) error
	ListUnseenUIDs(since time.Duration) ([]uint32, error)
	FetchMessage(uid uint32) (*imap.Message, error)
	MarkSeen(uid uint32) error
	Close() error
}
