package netflix

import (
	"net/url"
	"strings"
	"testing"
)

func TestSanitizeURLRedactsSensitiveValues(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		redacted []string
		kept     []string
	}{
		{
			name:     "Netflix link",
			raw:      "https://www.netflix.com/account/update-primary-location?nftoken=TOPSECRET&g=GSECRET&plain=kept#fragment",
			redacted: []string{"nftoken", "g"},
			kept:     []string{"plain"},
		},
		{
			name:     "mixed-case and compound keys",
			raw:      "https://www.netflix.com/path?AccessToken=ABC&session_id=XYZ&userEmail=me%40example.com&page=2",
			redacted: []string{"AccessToken", "session_id", "userEmail"},
			kept:     []string{"page"},
		},
		{
			name:     "embedded credentials and fragment",
			raw:      "https://user:password@www.netflix.com/path?token=ABC#secret-fragment",
			redacted: []string{"token"},
			kept:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeURL(tt.raw)
			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("sanitized URL is not valid: %v (%q)", err, got)
			}

			if parsed.User != nil {
				t.Errorf("embedded credentials were kept in %q", got)
			}
			if parsed.Fragment != "" {
				t.Errorf("fragment was kept in %q", got)
			}

			query := parsed.Query()
			for _, key := range tt.redacted {
				if value := query.Get(key); value != redactedValue {
					t.Errorf("query key %q = %q, want %q", key, value, redactedValue)
				}
			}
			for _, key := range tt.kept {
				if query.Get(key) == "" {
					t.Errorf("non-sensitive query key %q was lost in %q", key, got)
				}
			}

			for _, secret := range []string{"TOPSECRET", "GSECRET", "ABC", "XYZ", "password", "secret-fragment", "me@example.com"} {
				if strings.Contains(got, secret) {
					t.Errorf("sanitized URL %q still contains %q", got, secret)
				}
			}
		})
	}
}

func TestSanitizeURLUnparsableInputNeverLeaksQuery(t *testing.T) {
	raw := "https://www.netflix.com/%zz?nftoken=TOPSECRET"

	got := sanitizeURL(raw)
	if strings.Contains(got, "TOPSECRET") {
		t.Errorf("sanitized URL %q still contains the token", got)
	}
	if !strings.HasSuffix(got, "?"+redactedValue) {
		t.Errorf("sanitized URL = %q, want query replaced with %q", got, redactedValue)
	}
}

func TestConfirmationRequiresExplicitSuccess(t *testing.T) {
	tests := []struct {
		name  string
		state pageState
		want  bool
	}{
		{
			name: "redirect to error page",
			state: pageState{
				URL: "https://www.netflix.com/error",
			},
		},
		{
			name: "button disappears without confirmation",
			state: pageState{
				URL: "https://www.netflix.com/account/update-primary-location",
			},
		},
		{
			name:  "visible success marker",
			state: pageState{SuccessVisible: true},
			want:  true,
		},
		{
			name: "French confirmation text",
			state: pageState{
				BodyText: "Votre foyer Netflix a été mis à jour.",
			},
			want: true,
		},
		{
			name: "English confirmation text",
			state: pageState{
				BodyText: "Your Netflix Household has been updated.",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConfirmationSuccess(&tt.state); got != tt.want {
				t.Errorf("isConfirmationSuccess(%+v) = %t, want %t", tt.state, got, tt.want)
			}
		})
	}
}
