package claude

import (
	"os"
	"strings"

	"github.com/zhubert/erg/internal/secrets"
)

// Redactor replaces known secret values with a placeholder to prevent
// sensitive data from appearing in transcripts and stream log files.
type Redactor struct {
	secretValues []string
}

// NewRedactor creates a Redactor populated with secret values read from the
// current environment. Non-empty values of secrets.KnownSecretEnvVars are
// collected so they can be scrubbed from any text that passes through Redact.
func NewRedactor() *Redactor {
	var secretValues []string
	for _, name := range secrets.KnownSecretEnvVars {
		if val := os.Getenv(name); val != "" {
			secretValues = append(secretValues, val)
		}
	}
	return &Redactor{secretValues: secretValues}
}

// Redact replaces every occurrence of a known secret value in text with
// "[REDACTED]". Returns text unchanged when no secrets are configured.
func (r *Redactor) Redact(text string) string {
	for _, secret := range r.secretValues {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}
