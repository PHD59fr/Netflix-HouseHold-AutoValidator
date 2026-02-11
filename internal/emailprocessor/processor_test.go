package emailprocessor

import (
	"testing"
	"time"

	"netflix-household-validator/internal/models"
)

func TestIsEmailValid(t *testing.T) {
	p := &Processor{}
	now := time.Now()

	tests := []struct {
		name          string
		internalDate  time.Time
		expectedValid bool
	}{
		{
			name:          "Email within window (5 min ago)",
			internalDate:  now.Add(-5 * time.Minute),
			expectedValid: true,
		},
		{
			name:          "Email at edge of window (15 min ago)",
			internalDate:  now.Add(-15 * time.Minute),
			expectedValid: true,
		},
		{
			name:          "Email outside window (20 min ago)",
			internalDate:  now.Add(-20 * time.Minute),
			expectedValid: false,
		},
		{
			name:          "Email outside window (1 hour ago)",
			internalDate:  now.Add(-1 * time.Hour),
			expectedValid: false,
		},
		{
			name:          "Email with zero date (always valid)",
			internalDate:  time.Time{},
			expectedValid: true,
		},
		{
			name:          "Recent email (30 sec ago)",
			internalDate:  now.Add(-30 * time.Second),
			expectedValid: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			email := &models.Email{InternalDate: tt.internalDate}

			valid := p.isEmailValidAt(email, now)
			if valid != tt.expectedValid {
				var age time.Duration
				if !tt.internalDate.IsZero() {
					age = now.Sub(tt.internalDate)
				}
				t.Errorf("isEmailValidAt() = %v, want %v (date: %v, age: %v)",
					valid, tt.expectedValid, tt.internalDate, age)
			}
		})
	}
}

func TestEmailValidityWindow(t *testing.T) {
	expected := 15 * time.Minute
	if EmailValidityWindow != expected {
		t.Errorf("EmailValidityWindow = %v, want %v", EmailValidityWindow, expected)
	}
}
