package models

// BrowserResult represents the result of a browser automation attempt
type BrowserResult int

const (
	ResultFailed BrowserResult = iota
	ResultSuccess
	ResultExpired
	ResultAbort
)
