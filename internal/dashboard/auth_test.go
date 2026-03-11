package dashboard

import (
	"context"
	"fmt"
	"testing"

	iexec "github.com/zhubert/erg/internal/exec"
)

func TestFetchAuthInfo_Success(t *testing.T) {
	tests := []struct {
		name         string
		output       string
		wantEmail    string
		wantUUID     string
		wantSub      string
		wantLoggedIn bool
	}{
		{
			name: "logged in with full account info",
			output: `{
				"isLoggedIn": true,
				"claudeAiOAuthAccount": {
					"emailAddress": "user@example.com",
					"uuid": "abc-123",
					"subscription": "claude_pro"
				}
			}`,
			wantEmail:    "user@example.com",
			wantUUID:     "abc-123",
			wantSub:      "claude_pro",
			wantLoggedIn: true,
		},
		{
			name:         "not logged in",
			output:       `{"isLoggedIn": false}`,
			wantLoggedIn: false,
		},
		{
			name:         "logged in with no account details",
			output:       `{"isLoggedIn": true}`,
			wantLoggedIn: true,
		},
		{
			name: "extra unknown fields are ignored",
			output: `{
				"isLoggedIn": true,
				"unknownTopLevel": "ignored",
				"claudeAiOAuthAccount": {
					"emailAddress": "test@example.com",
					"unknownNested": 42
				}
			}`,
			wantEmail:    "test@example.com",
			wantLoggedIn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := iexec.NewMockExecutor(nil)
			ex.AddExactMatch("claude", []string{"auth", "status"}, iexec.MockResponse{
				Stdout: []byte(tt.output),
			})

			info, err := FetchAuthInfo(context.Background(), ex)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if info.Email != tt.wantEmail {
				t.Errorf("email: got %q, want %q", info.Email, tt.wantEmail)
			}
			if info.AccountUUID != tt.wantUUID {
				t.Errorf("account_uuid: got %q, want %q", info.AccountUUID, tt.wantUUID)
			}
			if info.Subscription != tt.wantSub {
				t.Errorf("subscription: got %q, want %q", info.Subscription, tt.wantSub)
			}
			if info.IsLoggedIn != tt.wantLoggedIn {
				t.Errorf("is_logged_in: got %v, want %v", info.IsLoggedIn, tt.wantLoggedIn)
			}
			if info.FetchedAt.IsZero() {
				t.Error("expected FetchedAt to be set")
			}
		})
	}
}

func TestFetchAuthInfo_InvalidJSON(t *testing.T) {
	ex := iexec.NewMockExecutor(nil)
	ex.AddExactMatch("claude", []string{"auth", "status"}, iexec.MockResponse{
		Stdout: []byte("not valid json"),
	})

	_, err := FetchAuthInfo(context.Background(), ex)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFetchAuthInfo_EmptyOutput(t *testing.T) {
	ex := iexec.NewMockExecutor(nil)
	ex.AddExactMatch("claude", []string{"auth", "status"}, iexec.MockResponse{
		Stdout: []byte(""),
	})

	_, err := FetchAuthInfo(context.Background(), ex)
	if err == nil {
		t.Error("expected error for empty output")
	}
}

func TestFetchAuthInfo_CommandError(t *testing.T) {
	ex := iexec.NewMockExecutor(nil)
	ex.AddExactMatch("claude", []string{"auth", "status"}, iexec.MockResponse{
		Err: fmt.Errorf("command not found"),
	})

	_, err := FetchAuthInfo(context.Background(), ex)
	if err == nil {
		t.Error("expected error when command fails")
	}
}
