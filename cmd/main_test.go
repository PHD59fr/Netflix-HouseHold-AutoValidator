package main

import (
	"context"
	"testing"
	"time"

	"github.com/emersion/go-imap"
)

type batchClient struct {
	uids    []uint32
	fetched []uint32
}

func (c *batchClient) Connect(context.Context, string) error       { return nil }
func (c *batchClient) Login(context.Context, string, string) error { return nil }
func (c *batchClient) SelectMailbox(string) error                  { return nil }
func (c *batchClient) Close() error                                { return nil }
func (c *batchClient) WaitForNewMail(context.Context) error        { return nil }
func (c *batchClient) MarkSeen(uint32) error                       { return nil }

func (c *batchClient) ListUnseenUIDs(time.Duration) ([]uint32, error) {
	return c.uids, nil
}

func (c *batchClient) FetchMessage(uid uint32) (*imap.Message, error) {
	c.fetched = append(c.fetched, uid)
	// An empty message is rejected by the parser after the fetch. The test
	// exercises batching without involving browser automation.
	return &imap.Message{}, nil
}

func TestFetchAndProcessEmailsResumesAfterLimit(t *testing.T) {
	client := &batchClient{}
	for uid := uint32(1); uid <= maxEmailsPerCycle+1; uid++ {
		client.uids = append(client.uids, uid)
	}
	processed := make(map[uint32]struct{})

	more, err := fetchAndProcessEmails(context.Background(), client, nil, processed)
	if err != nil {
		t.Fatalf("first processing pass: %v", err)
	}
	if !more || len(client.fetched) != maxEmailsPerCycle {
		t.Fatalf("first pass: more=%t, fetched=%d; want true and %d", more, len(client.fetched), maxEmailsPerCycle)
	}

	more, err = fetchAndProcessEmails(context.Background(), client, nil, processed)
	if err != nil {
		t.Fatalf("second processing pass: %v", err)
	}
	if more || len(client.fetched) != maxEmailsPerCycle+1 || client.fetched[len(client.fetched)-1] != maxEmailsPerCycle+1 {
		t.Fatalf("second pass: more=%t, fetched=%v; want UID %d handled last", more, client.fetched, maxEmailsPerCycle+1)
	}
}
