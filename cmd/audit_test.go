package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// sampleLines is a set of JSON log lines for testing audit filters.
var sampleLines = []string{
	`{"time":"2026-03-15T10:00:00Z","level":"INFO","msg":"queued new issue","event":"session.created","workItem":"owner/repo-1","repo":"owner/repo"}`,
	`{"time":"2026-03-15T10:05:00Z","level":"INFO","msg":"PR created","event":"pr.created","workItem":"owner/repo-1","repo":"owner/repo"}`,
	`{"time":"2026-03-15T10:10:00Z","level":"INFO","msg":"PR merged","event":"pr.merged","workItem":"owner/repo-1","repo":"owner/repo"}`,
	`{"time":"2026-03-15T11:00:00Z","level":"INFO","msg":"queued new issue","event":"session.created","workItem":"other/repo-2","repo":"other/repo"}`,
	`{"time":"2026-03-15T11:05:00Z","level":"WARN","msg":"worker completed with error","event":"session.failed","workItem":"other/repo-2","repo":"other/repo"}`,
	`not a json line`,
	`{"time":"2026-03-15T12:00:00Z","level":"INFO","msg":"logger initialized"}`,
}

func makeReader(lines []string) *strings.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

func TestAuditTable_NoFilter(t *testing.T) {
	resetAuditFlags()
	var buf bytes.Buffer
	err := streamAuditTable(&buf, makeReader(sampleLines), time.Time{})
	if err != nil {
		t.Fatalf("streamAuditTable returned error: %v", err)
	}
	out := buf.String()
	// All JSON entries should appear (non-JSON line skipped)
	if !strings.Contains(out, "session.created") {
		t.Error("output should contain session.created events")
	}
	if !strings.Contains(out, "pr.merged") {
		t.Error("output should contain pr.merged events")
	}
	if strings.Contains(out, "not a json line") {
		t.Error("non-JSON line should be skipped")
	}
}

func TestAuditTable_FilterByEvent(t *testing.T) {
	resetAuditFlags()
	auditEvent = "pr.merged"
	defer resetAuditFlags()

	var buf bytes.Buffer
	err := streamAuditTable(&buf, makeReader(sampleLines), time.Time{})
	if err != nil {
		t.Fatalf("streamAuditTable returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "pr.merged") {
		t.Error("output should contain pr.merged event")
	}
	if strings.Contains(out, "session.created") {
		t.Error("output should not contain session.created events when filtering for pr.merged")
	}
}

func TestAuditTable_FilterByWorkItem(t *testing.T) {
	resetAuditFlags()
	auditWorkitem = "owner/repo-1"
	defer resetAuditFlags()

	var buf bytes.Buffer
	err := streamAuditTable(&buf, makeReader(sampleLines), time.Time{})
	if err != nil {
		t.Fatalf("streamAuditTable returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "owner/repo-1") {
		t.Error("output should contain owner/repo-1 entries")
	}
	if strings.Contains(out, "other/repo-2") {
		t.Error("output should not contain other/repo-2 entries")
	}
}

func TestAuditTable_FilterByRepo(t *testing.T) {
	resetAuditFlags()
	auditRepo = "other/repo"
	defer resetAuditFlags()

	var buf bytes.Buffer
	err := streamAuditTable(&buf, makeReader(sampleLines), time.Time{})
	if err != nil {
		t.Fatalf("streamAuditTable returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "session.failed") {
		t.Error("output should contain session.failed for other/repo")
	}
	if strings.Contains(out, "pr.merged") {
		t.Error("output should not contain pr.merged (owner/repo) when filtering for other/repo")
	}
}

func TestAuditTable_FilterBySince(t *testing.T) {
	resetAuditFlags()

	// Use a since time that excludes entries before 11:00
	since, _ := time.Parse(time.RFC3339, "2026-03-15T11:00:00Z")

	var buf bytes.Buffer
	err := streamAuditTable(&buf, makeReader(sampleLines), since)
	if err != nil {
		t.Fatalf("streamAuditTable returned error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "pr.created") {
		t.Error("output should not contain pr.created (10:05) which is before since time")
	}
	if !strings.Contains(out, "session.created") {
		t.Error("output should contain session.created at 11:00 which equals since time")
	}
}

func TestAuditTable_NoMatches(t *testing.T) {
	resetAuditFlags()
	auditEvent = "nonexistent.event"
	defer resetAuditFlags()

	var buf bytes.Buffer
	err := streamAuditTable(&buf, makeReader(sampleLines), time.Time{})
	if err != nil {
		t.Fatalf("streamAuditTable returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "no matching entries") {
		t.Error("output should indicate no matching entries")
	}
}

func TestAuditJSON_Output(t *testing.T) {
	resetAuditFlags()
	auditEvent = "pr.merged"
	defer resetAuditFlags()

	var buf bytes.Buffer
	err := streamAuditJSON(&buf, makeReader(sampleLines), time.Time{})
	if err != nil {
		t.Fatalf("streamAuditJSON returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "pr.merged") {
		t.Error("output should contain pr.merged event")
	}
	if strings.Contains(out, "session.created") {
		t.Error("output should not contain session.created events when filtering for pr.merged")
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
		approx  time.Duration
	}{
		{"24h", false, 24 * time.Hour},
		{"7d", false, 7 * 24 * time.Hour},
		{"1d", false, 24 * time.Hour},
		{"30m", false, 30 * time.Minute},
		{"0d", true, 0},
		{"-1d", true, 0},
		{"bad", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.approx {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.approx)
			}
		})
	}
}

func TestMatchesFilters_EventFilter(t *testing.T) {
	resetAuditFlags()
	auditEvent = "session.created"
	defer resetAuditFlags()

	match := map[string]any{"event": "session.created", "workItem": "x"}
	noMatch := map[string]any{"event": "pr.merged", "workItem": "x"}

	if !matchesFilters(match, time.Time{}) {
		t.Error("entry with matching event should pass filter")
	}
	if matchesFilters(noMatch, time.Time{}) {
		t.Error("entry with non-matching event should not pass filter")
	}
}

func TestMatchesFilters_WorkItemFilter(t *testing.T) {
	resetAuditFlags()
	auditWorkitem = "owner/repo-1"
	defer resetAuditFlags()

	match := map[string]any{"workItem": "owner/repo-1"}
	noMatch := map[string]any{"workItem": "other/repo-2"}

	if !matchesFilters(match, time.Time{}) {
		t.Error("entry with matching workItem should pass filter")
	}
	if matchesFilters(noMatch, time.Time{}) {
		t.Error("entry with non-matching workItem should not pass filter")
	}
}

func TestFormatAuditTime(t *testing.T) {
	ts := "2026-03-15T10:05:30Z"
	got := formatAuditTime(ts)
	if got != "2026-03-15 10:05:30" {
		t.Errorf("formatAuditTime(%q) = %q, want %q", ts, got, "2026-03-15 10:05:30")
	}

	got = formatAuditTime(nil)
	if got != "" {
		t.Errorf("formatAuditTime(nil) should return empty string, got %q", got)
	}
}

// resetAuditFlags resets all audit command flags to their zero values.
func resetAuditFlags() {
	auditEvent = ""
	auditWorkitem = ""
	auditRepo = ""
	auditSince = ""
	auditJSON = false
}
