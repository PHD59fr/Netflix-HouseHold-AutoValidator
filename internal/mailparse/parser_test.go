package mailparse

import (
	"strings"
	"testing"
)

func TestDecodeHeader(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "Plain ASCII",
			input:    "Hello World",
			expected: "Hello World",
			wantErr:  false,
		},
		{
			name:     "UTF-8 encoded",
			input:    "=?UTF-8?Q?Important_:_comment_mettre_=C3=A0_jour?=",
			expected: "Important : comment mettre à jour",
			wantErr:  false,
		},
		{
			name:     "ISO-8859-1 encoded",
			input:    "=?ISO-8859-1?Q?Caf=E9?=",
			expected: "Café",
			wantErr:  false,
		},
		{
			name:     "Base64 encoded",
			input:    "=?UTF-8?B?SGVsbG8gV29ybGQ=?=",
			expected: "Hello World",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeHeader(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("DecodeHeader() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractLinks(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "Single HTTP link",
			text:     "Click here: http://example.com",
			expected: []string{"http://example.com"},
		},
		{
			name:     "Single HTTPS link",
			text:     "Visit https://netflix.com/update-primary-location?token=abc123",
			expected: []string{"https://netflix.com/update-primary-location?token=abc123"},
		},
		{
			name:     "Multiple links",
			text:     "Check http://example.com and https://netflix.com/test",
			expected: []string{"http://example.com", "https://netflix.com/test"},
		},
		{
			name:     "Link in HTML",
			text:     `<a href="https://netflix.com/update">Click</a>`,
			expected: []string{"https://netflix.com/update"},
		},
		{
			name:     "No links",
			text:     "This is plain text without any links",
			expected: nil,
		},
		{
			name: "Real Netflix email body",
			text: `Bonjour,

Pour continuer à regarder Netflix, vous devez mettre à jour votre foyer Netflix.

https://www.netflix.com/account/update-primary-location?nftoken=BQABAAEBEBguLT-vNa5example

Si vous ne reconnaissez pas cette activité, visitez notre Centre d'aide.

L'équipe Netflix`,
			expected: []string{"https://www.netflix.com/account/update-primary-location?nftoken=BQABAAEBEBguLT-vNa5example"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractLinks(tt.text)
			if len(got) != len(tt.expected) {
				t.Errorf("ExtractLinks() returned %d links, want %d\nGot: %v\nWant: %v",
					len(got), len(tt.expected), got, tt.expected)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("ExtractLinks()[%d] = %v, want %v", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestExtractEmailAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple email",
			input:    "info@account.netflix.com",
			expected: "info@account.netflix.com",
		},
		{
			name:     "Email with name",
			input:    "Netflix <info@account.netflix.com>",
			expected: "info@account.netflix.com",
		},
		{
			name:     "Email with quotes",
			input:    `"Netflix Team" <info@account.netflix.com>`,
			expected: "info@account.netflix.com",
		},
		{
			name:     "No email",
			input:    "Just some text",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractEmailAddress(tt.input)
			if got != tt.expected {
				t.Errorf("extractEmailAddress() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractLinks_Netflix(t *testing.T) {
	body := "Visit: https://www.netflix.com/account/update-primary-location?nftoken=ABC123"
	links := ExtractLinks(body)

	if len(links) != 1 {
		t.Fatalf("Expected 1 link, got %d", len(links))
	}

	if !strings.Contains(links[0], "update-primary-location") {
		t.Errorf("Expected link to contain 'update-primary-location', got %s", links[0])
	}
}
