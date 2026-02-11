package netflix

import (
	"netflix-household-validator/internal/models"
	"testing"
	"time"
)

type MockBrowser struct {
	Result models.BrowserResult
	Err    error
}

func (m *MockBrowser) OpenUpdatePrimaryLocation(link, email, password string, traceID string) (models.BrowserResult, error) {
	return m.Result, m.Err
}

func TestHandleEmail_FilterBySender(t *testing.T) {
	cfg := &models.Config{
		TargetFrom:    "info@account.netflix.com",
		TargetSubject: "Test Subject",
	}

	mockBrowser := &MockBrowser{Result: models.ResultSuccess}
	svc := NewService(mockBrowser, cfg)

	email := &models.Email{
		From:      "wrong@sender.com",
		Subject:   "Test Subject",
		BodyText:  "https://netflix.com/update-primary-location?token=123",
		ToPrimary: "user@example.com",
		TraceID:   "test-trace",
	}

	handled := svc.HandleEmail(email)
	if handled {
		t.Error("Expected email to be rejected due to wrong sender")
	}
}

func TestHandleEmail_FilterBySubject(t *testing.T) {
	cfg := &models.Config{
		TargetFrom:    "info@account.netflix.com",
		TargetSubject: "Expected Subject",
	}

	mockBrowser := &MockBrowser{Result: models.ResultSuccess}
	svc := NewService(mockBrowser, cfg)

	email := &models.Email{
		From:      "info@account.netflix.com",
		Subject:   "Wrong Subject",
		BodyText:  "https://netflix.com/update-primary-location?token=123",
		ToPrimary: "user@example.com",
		TraceID:   "test-trace",
	}

	handled := svc.HandleEmail(email)
	if handled {
		t.Error("Expected email to be rejected due to wrong subject")
	}
}

func TestHandleEmail_EmptyBody(t *testing.T) {
	cfg := &models.Config{
		TargetFrom:    "info@account.netflix.com",
		TargetSubject: "Test Subject",
	}

	mockBrowser := &MockBrowser{Result: models.ResultSuccess}
	svc := NewService(mockBrowser, cfg)

	email := &models.Email{
		From:      "info@account.netflix.com",
		Subject:   "Test Subject",
		BodyText:  "",
		ToPrimary: "user@example.com",
		TraceID:   "test-trace",
	}

	handled := svc.HandleEmail(email)
	if handled {
		t.Error("Expected email to be rejected due to empty body")
	}
}

func TestHandleEmail_NoUpdateLink(t *testing.T) {
	cfg := &models.Config{
		TargetFrom:    "info@account.netflix.com",
		TargetSubject: "Test Subject",
	}

	mockBrowser := &MockBrowser{Result: models.ResultSuccess}
	svc := NewService(mockBrowser, cfg)

	email := &models.Email{
		From:      "info@account.netflix.com",
		Subject:   "Test Subject",
		BodyText:  "Just some text without update-primary-location link",
		ToPrimary: "user@example.com",
		TraceID:   "test-trace",
	}

	handled := svc.HandleEmail(email)
	if handled {
		t.Error("Expected email to be rejected due to missing update-primary-location link")
	}
}

func TestHandleEmail_Success(t *testing.T) {
	cfg := &models.Config{
		TargetFrom:      "info@account.netflix.com",
		TargetSubject:   "Test Subject",
		FilterByAccount: false,
	}

	mockBrowser := &MockBrowser{Result: models.ResultSuccess}
	svc := NewService(mockBrowser, cfg)

	email := &models.Email{
		From:         "info@account.netflix.com",
		Subject:      "Test Subject",
		BodyText:     "Click here: https://netflix.com/update-primary-location?token=abc123",
		ToPrimary:    "user@example.com",
		TraceID:      "test-trace",
		InternalDate: time.Now(),
	}

	handled := svc.HandleEmail(email)
	if !handled {
		t.Error("Expected email to be handled successfully")
	}
}

func TestHandleEmail_FilterByAccount_Match(t *testing.T) {
	cfg := &models.Config{
		TargetFrom:      "info@account.netflix.com",
		TargetSubject:   "Test Subject",
		FilterByAccount: true,
		NetflixAuth: []models.NetflixAccount{
			{Email: "user1@example.com", Password: "pass1"},
			{Email: "user2@example.com", Password: "pass2"},
		},
	}

	mockBrowser := &MockBrowser{Result: models.ResultSuccess}
	svc := NewService(mockBrowser, cfg)

	email := &models.Email{
		From:      "info@account.netflix.com",
		Subject:   "Test Subject",
		BodyText:  "https://netflix.com/update-primary-location?token=abc",
		ToPrimary: "user2@example.com",
		TraceID:   "test-trace",
	}

	handled := svc.HandleEmail(email)
	if !handled {
		t.Error("Expected email to be handled for matching account")
	}
}

func TestHandleEmail_FilterByAccount_NoMatch(t *testing.T) {
	cfg := &models.Config{
		TargetFrom:      "info@account.netflix.com",
		TargetSubject:   "Test Subject",
		FilterByAccount: true,
		NetflixAuth: []models.NetflixAccount{
			{Email: "user1@example.com", Password: "pass1"},
		},
	}

	mockBrowser := &MockBrowser{Result: models.ResultSuccess}
	svc := NewService(mockBrowser, cfg)

	email := &models.Email{
		From:      "info@account.netflix.com",
		Subject:   "Test Subject",
		BodyText:  "https://netflix.com/update-primary-location?token=abc",
		ToPrimary: "unknown@example.com",
		TraceID:   "test-trace",
	}

	handled := svc.HandleEmail(email)
	if handled {
		t.Error("Expected email to be rejected due to no matching account")
	}
}

func TestHandleEmail_BrowserResultExpired(t *testing.T) {
	cfg := &models.Config{
		TargetFrom:      "info@account.netflix.com",
		TargetSubject:   "Test Subject",
		FilterByAccount: false,
	}

	mockBrowser := &MockBrowser{Result: models.ResultExpired}
	svc := NewService(mockBrowser, cfg)

	email := &models.Email{
		From:      "info@account.netflix.com",
		Subject:   "Test Subject",
		BodyText:  "https://netflix.com/update-primary-location?token=abc",
		ToPrimary: "user@example.com",
		TraceID:   "test-trace",
	}

	handled := svc.HandleEmail(email)
	if !handled {
		t.Error("Expected expired result to be treated as handled")
	}
}

func TestHandleEmail_BrowserResultAbort(t *testing.T) {
	cfg := &models.Config{
		TargetFrom:      "info@account.netflix.com",
		TargetSubject:   "Test Subject",
		FilterByAccount: false,
	}

	mockBrowser := &MockBrowser{Result: models.ResultAbort}
	svc := NewService(mockBrowser, cfg)

	email := &models.Email{
		From:      "info@account.netflix.com",
		Subject:   "Test Subject",
		BodyText:  "https://netflix.com/update-primary-location?token=abc",
		ToPrimary: "user@example.com",
		TraceID:   "test-trace",
	}

	handled := svc.HandleEmail(email)
	if handled {
		t.Error("Expected abort result to return false")
	}
}
