package models

import "time"

// Email represents a normalized parsed email message
type Email struct {
	UID          uint32
	From         string
	To           []string
	ToPrimary    string
	Subject      string
	BodyText     string
	InternalDate time.Time
	TraceID      string
}
