package claude

import (
	"slices"
	"strings"
	"testing"

	"github.com/zhubert/erg/internal/secrets"
)

func TestRedactor_Redact(t *testing.T) {
	tests := []struct {
		name         string
		secretValues []string
		input        string
		want         string
	}{
		{
			name:         "no secrets configured",
			secretValues: nil,
			input:        "hello world",
			want:         "hello world",
		},
		{
			name:         "single secret replaced",
			secretValues: []string{"sk-ant-abc123"},
			input:        `{"api_key":"sk-ant-abc123"}`,
			want:         `{"api_key":"[REDACTED]"}`,
		},
		{
			name:         "multiple secrets replaced",
			secretValues: []string{"token-abc", "key-xyz"},
			input:        "use token-abc and key-xyz",
			want:         "use [REDACTED] and [REDACTED]",
		},
		{
			name:         "secret appears multiple times",
			secretValues: []string{"s3cr3t"},
			input:        "s3cr3t is s3cr3t",
			want:         "[REDACTED] is [REDACTED]",
		},
		{
			name:         "no secret present in text",
			secretValues: []string{"sk-ant-abc123"},
			input:        "no sensitive data here",
			want:         "no sensitive data here",
		},
		{
			name:         "empty input",
			secretValues: []string{"sk-ant-abc123"},
			input:        "",
			want:         "",
		},
		{
			name:         "secret in JSON stream line",
			secretValues: []string{"sk-ant-realkey"},
			input:        `{"type":"result","content":"key is sk-ant-realkey done"}`,
			want:         `{"type":"result","content":"key is [REDACTED] done"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Redactor{secretValues: tc.secretValues}
			got := r.Redact(tc.input)
			if got != tc.want {
				t.Errorf("Redact(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNewRedactor_ReadsEnvVars(t *testing.T) {
	// Set known env var and confirm NewRedactor picks it up
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")
	t.Setenv("GITHUB_TOKEN", "ghp_testtoken")

	r := NewRedactor()

	if len(r.secretValues) < 2 {
		t.Fatalf("expected at least 2 secrets, got %d", len(r.secretValues))
	}

	input := "key=sk-ant-test-key token=ghp_testtoken"
	got := r.Redact(input)

	if strings.Contains(got, "sk-ant-test-key") {
		t.Error("ANTHROPIC_API_KEY value not redacted")
	}
	if strings.Contains(got, "ghp_testtoken") {
		t.Error("GITHUB_TOKEN value not redacted")
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Error("expected [REDACTED] placeholder in output")
	}
}

func TestNewRedactor_IgnoresEmptyEnvVars(t *testing.T) {
	// Unset all known vars to ensure empty values are skipped
	for _, name := range secrets.KnownSecretEnvVars {
		t.Setenv(name, "")
	}

	r := NewRedactor()

	if len(r.secretValues) != 0 {
		t.Errorf("expected 0 secrets when env vars are empty, got %d", len(r.secretValues))
	}
}

func TestNewRedactor_AllKnownVarsRecognised(t *testing.T) {
	// Each known env var should be collected when set
	for _, name := range secrets.KnownSecretEnvVars {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "super-secret-value")
			r := NewRedactor()
			found := slices.Contains(r.secretValues, "super-secret-value")
			if !found {
				t.Errorf("env var %s not collected by NewRedactor", name)
			}
		})
	}
}
