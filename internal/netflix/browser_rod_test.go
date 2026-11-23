package netflix

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
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

func TestConfirmationNavigation(t *testing.T) {
	bin, err := exec.LookPath("chromium")
	if err != nil {
		t.Skip("requires installed Chromium")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	l := launcher.New().Bin(bin).Context(ctx).NoSandbox(true)
	control, err := l.Launch()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Cleanup()
	browser := rod.New().Context(ctx).ControlURL(control).MustConnect()
	defer browser.MustClose()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/start":
			// The element keeps moving so Rod cannot observe two stable frames.
			fmt.Fprint(w, `<style>@keyframes move { to { transform: translateX(150px) } } button { animation: move 1s linear infinite alternate }</style>
                <button data-uia="update-household-confirm-cta" onclick="location.pathname=new URLSearchParams(location.search).get('to')">Confirm</button>`)
		case "/ok":
			fmt.Fprint(w, `<p data-uia="upl-success">Done</p>`)
		case "/invalid":
			fmt.Fprint(w, `<p data-uia="upl-invalid-token">Expired</p>`)
		default:
			fmt.Fprint(w, "Unknown")
		}
	}))
	defer server.Close()
	t.Run("covered button", func(t *testing.T) {
		page := browser.MustPage()
		defer page.MustClose()
		page.MustEval("(html) => document.body.innerHTML = html", `<button data-uia="update-household-confirm-cta" onclick="this.dataset.uia='upl-success'">Confirm</button>
            <div style="position:fixed;inset:0"></div>`)
		got, err := racePageElements(page, 750*time.Millisecond, "local-test")
		if got != outcomeUnknown || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("covered button: got %v, %v", got, err)
		}
	})

	t.Run("late acknowledgement", func(t *testing.T) {
		page := browser.MustPage()
		defer page.MustClose()
		page.MustEval("(html) => document.body.innerHTML = html", `<button data-uia="update-household-confirm-cta" onclick="
            this.removeAttribute('data-uia');
            const end = performance.now() + 5500;
            while (performance.now() < end) {}
            this.dataset.uia = 'upl-success';
        ">Confirm</button>`)
		got, err := racePageElements(page, 8*time.Second, "local-test")
		if got != outcomeConfirmed || err != nil {
			t.Fatalf("confirmation after evaluation timeout: got %v, %v", got, err)
		}
	})

	for _, tc := range []struct {
		path string
		want pageOutcome
	}{
		{"/ok", outcomeConfirmed}, {"/invalid", outcomeExpired}, {"/login", outcomeLogin}, {"/error", outcomeUnknown},
	} {
		t.Run(tc.path, func(t *testing.T) {
			page := browser.MustPage(server.URL + "/start?to=" + tc.path)
			defer page.MustClose()
			phase, stop := context.WithTimeout(ctx, 3*time.Second)
			defer stop()
			got, err := racePageElements(page.Context(phase), 3*time.Second, "local-test")
			if got != tc.want || (tc.want == outcomeUnknown && !errors.Is(err, context.DeadlineExceeded)) || (tc.want != outcomeUnknown && err != nil) {
				t.Fatalf("got outcome %v, err %v; want %v", got, err, tc.want)
			}
		})
	}
}
