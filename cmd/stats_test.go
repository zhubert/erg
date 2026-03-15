package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/zhubert/erg/internal/config"
	"github.com/zhubert/erg/internal/daemonstate"
)

// ---- computeSessionStats ----

func TestComputeSessionStats_Empty(t *testing.T) {
	stats := computeSessionStats(nil)
	if stats.Total != 0 {
		t.Errorf("expected Total=0, got %d", stats.Total)
	}
	if stats.Completed != 0 || stats.Failed != 0 || stats.Active != 0 || stats.Queued != 0 {
		t.Errorf("expected all counts=0 for empty input")
	}
	if len(stats.MergeDurations) != 0 {
		t.Errorf("expected no merge durations for empty input")
	}
}

func TestComputeSessionStats_Counts(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(-10 * time.Minute)
	items := []daemonstate.WorkItem{
		{State: daemonstate.WorkItemCompleted, CreatedAt: now.Add(-1 * time.Hour), CompletedAt: &completedAt},
		{State: daemonstate.WorkItemCompleted, CreatedAt: now.Add(-2 * time.Hour), CompletedAt: &completedAt},
		{State: daemonstate.WorkItemFailed},
		{State: daemonstate.WorkItemActive},
		{State: daemonstate.WorkItemQueued},
	}
	stats := computeSessionStats(items)
	if stats.Total != 5 {
		t.Errorf("expected Total=5, got %d", stats.Total)
	}
	if stats.Completed != 2 {
		t.Errorf("expected Completed=2, got %d", stats.Completed)
	}
	if stats.Failed != 1 {
		t.Errorf("expected Failed=1, got %d", stats.Failed)
	}
	if stats.Active != 1 {
		t.Errorf("expected Active=1, got %d", stats.Active)
	}
	if stats.Queued != 1 {
		t.Errorf("expected Queued=1, got %d", stats.Queued)
	}
}

func TestComputeSessionStats_MergeDurations(t *testing.T) {
	now := time.Now()
	dur1 := 30 * time.Minute
	dur2 := 90 * time.Minute
	t1 := now.Add(-dur1)
	t2 := now.Add(-dur2)
	items := []daemonstate.WorkItem{
		{State: daemonstate.WorkItemCompleted, CreatedAt: now.Add(-dur1 - time.Hour), CompletedAt: &t1},
		{State: daemonstate.WorkItemCompleted, CreatedAt: now.Add(-dur2 - time.Hour), CompletedAt: &t2},
	}
	stats := computeSessionStats(items)
	if len(stats.MergeDurations) != 2 {
		t.Errorf("expected 2 merge durations, got %d", len(stats.MergeDurations))
	}
}

func TestComputeSessionStats_NoCostWhenZero(t *testing.T) {
	items := []daemonstate.WorkItem{
		{State: daemonstate.WorkItemCompleted, CostUSD: 0, InputTokens: 0, OutputTokens: 0},
	}
	stats := computeSessionStats(items)
	if len(stats.CostItems) != 0 {
		t.Errorf("expected no cost items when cost/tokens are zero")
	}
}

func TestComputeSessionStats_CostItemsSortedDesc(t *testing.T) {
	items := []daemonstate.WorkItem{
		{State: daemonstate.WorkItemCompleted, CostUSD: 0.10, IssueRef: config.IssueRef{ID: "1", Title: "cheap"}},
		{State: daemonstate.WorkItemCompleted, CostUSD: 0.80, IssueRef: config.IssueRef{ID: "2", Title: "expensive"}},
		{State: daemonstate.WorkItemCompleted, CostUSD: 0.40, IssueRef: config.IssueRef{ID: "3", Title: "mid"}},
	}
	stats := computeSessionStats(items)
	if len(stats.CostItems) != 3 {
		t.Fatalf("expected 3 cost items, got %d", len(stats.CostItems))
	}
	if stats.CostItems[0].CostUSD != 0.80 {
		t.Errorf("expected first cost item to be most expensive, got %.2f", stats.CostItems[0].CostUSD)
	}
	if stats.CostItems[2].CostUSD != 0.10 {
		t.Errorf("expected last cost item to be cheapest, got %.2f", stats.CostItems[2].CostUSD)
	}
}

func TestComputeSessionStats_FailedItems(t *testing.T) {
	items := []daemonstate.WorkItem{
		{State: daemonstate.WorkItemFailed, CurrentStep: "coding", ErrorMessage: "timeout"},
		{State: daemonstate.WorkItemFailed, CurrentStep: "coding", ErrorMessage: "timeout"},
		{State: daemonstate.WorkItemFailed, CurrentStep: "await_ci"},
	}
	stats := computeSessionStats(items)
	if len(stats.FailedItems) != 3 {
		t.Errorf("expected 3 failed items, got %d", len(stats.FailedItems))
	}
}

func TestComputeSessionStats_FeedbackItems(t *testing.T) {
	items := []daemonstate.WorkItem{
		{State: daemonstate.WorkItemCompleted, FeedbackRounds: 2},
		{State: daemonstate.WorkItemCompleted, FeedbackRounds: 0},
		{State: daemonstate.WorkItemCompleted, FeedbackRounds: 3},
	}
	stats := computeSessionStats(items)
	if len(stats.FeedbackItems) != 2 {
		t.Errorf("expected 2 feedback items (rounds>0), got %d", len(stats.FeedbackItems))
	}
}

// ---- formatStats output ----

func TestFormatStats_EmptyState(t *testing.T) {
	var buf bytes.Buffer
	stats := computeSessionStats(nil)
	formatStats(&buf, stats)
	out := buf.String()
	if !strings.Contains(out, "Sessions:") {
		t.Errorf("expected 'Sessions:' in output, got:\n%s", out)
	}
	// No merge/spend/failure sections for empty state
	if strings.Contains(out, "Time to Merge") {
		t.Errorf("unexpected 'Time to Merge' section for empty state")
	}
	if strings.Contains(out, "Token Spend") {
		t.Errorf("unexpected 'Token Spend' section for empty state")
	}
	if strings.Contains(out, "Failure Analysis") {
		t.Errorf("unexpected 'Failure Analysis' section for empty state")
	}
}

func TestFormatStats_AllCompleted(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(-30 * time.Minute)
	items := []daemonstate.WorkItem{
		{
			State:        daemonstate.WorkItemCompleted,
			CreatedAt:    now.Add(-90 * time.Minute),
			CompletedAt:  &completedAt,
			CostUSD:      0.42,
			InputTokens:  20000,
			OutputTokens: 5000,
			IssueRef:     config.IssueRef{Source: "github", ID: "10", Title: "Fix bug"},
		},
	}
	var buf bytes.Buffer
	stats := computeSessionStats(items)
	formatStats(&buf, stats)
	out := buf.String()

	if !strings.Contains(out, "1 total") {
		t.Errorf("expected '1 total' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Time to Merge") {
		t.Errorf("expected 'Time to Merge' section, got:\n%s", out)
	}
	if !strings.Contains(out, "Token Spend") {
		t.Errorf("expected 'Token Spend' section, got:\n%s", out)
	}
	if !strings.Contains(out, "#10") {
		t.Errorf("expected issue #10 in token spend, got:\n%s", out)
	}
}

func TestFormatStats_FailureSection(t *testing.T) {
	items := []daemonstate.WorkItem{
		{State: daemonstate.WorkItemFailed, CurrentStep: "coding", ErrorMessage: "context cancelled"},
		{State: daemonstate.WorkItemFailed, CurrentStep: "coding", ErrorMessage: "context cancelled"},
		{State: daemonstate.WorkItemFailed, CurrentStep: "await_ci"},
	}
	var buf bytes.Buffer
	stats := computeSessionStats(items)
	formatStats(&buf, stats)
	out := buf.String()

	if !strings.Contains(out, "Failure Analysis") {
		t.Errorf("expected 'Failure Analysis' section, got:\n%s", out)
	}
	if !strings.Contains(out, "coding") {
		t.Errorf("expected 'coding' step in failure analysis, got:\n%s", out)
	}
	if !strings.Contains(out, "context cancelled") {
		t.Errorf("expected error message in failure analysis, got:\n%s", out)
	}
}

func TestFormatStats_SuccessRate(t *testing.T) {
	now := time.Now()
	completedAt := now
	items := []daemonstate.WorkItem{
		{State: daemonstate.WorkItemCompleted, CreatedAt: now.Add(-time.Hour), CompletedAt: &completedAt},
		{State: daemonstate.WorkItemCompleted, CreatedAt: now.Add(-time.Hour), CompletedAt: &completedAt},
		{State: daemonstate.WorkItemCompleted, CreatedAt: now.Add(-time.Hour), CompletedAt: &completedAt},
		{State: daemonstate.WorkItemFailed},
	}
	var buf bytes.Buffer
	stats := computeSessionStats(items)
	formatStats(&buf, stats)
	out := buf.String()
	// 3/4 = 75%
	if !strings.Contains(out, "75.0%") {
		t.Errorf("expected 75.0%% success rate, got:\n%s", out)
	}
}

func TestFormatStats_FeedbackSection(t *testing.T) {
	items := []daemonstate.WorkItem{
		{State: daemonstate.WorkItemCompleted, FeedbackRounds: 1},
		{State: daemonstate.WorkItemCompleted, FeedbackRounds: 3},
	}
	var buf bytes.Buffer
	stats := computeSessionStats(items)
	formatStats(&buf, stats)
	out := buf.String()
	if !strings.Contains(out, "Feedback Rounds") {
		t.Errorf("expected 'Feedback Rounds' section, got:\n%s", out)
	}
	if !strings.Contains(out, "Max: 3") {
		t.Errorf("expected 'Max: 3' in feedback section, got:\n%s", out)
	}
}

// ---- formatDuration ----

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "< 1m"},
		{30 * time.Second, "< 1m"},
		{time.Minute, "1m"},
		{90 * time.Minute, "1h 30m"},
		{2 * time.Hour, "2h"},
		{time.Hour + 23*time.Minute, "1h 23m"},
	}
	for _, tc := range tests {
		got := formatDuration(tc.d)
		if got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// ---- formatWorkItemLabel ----

func TestFormatWorkItemLabel_GitHub(t *testing.T) {
	item := daemonstate.WorkItem{
		IssueRef: config.IssueRef{Source: "github", ID: "42", Title: "Fix login bug"},
	}
	got := formatWorkItemLabel(item)
	if !strings.HasPrefix(got, "#42") {
		t.Errorf("expected label to start with '#42', got %q", got)
	}
}

func TestFormatWorkItemLabel_Truncation(t *testing.T) {
	item := daemonstate.WorkItem{
		IssueRef: config.IssueRef{Source: "github", ID: "1", Title: "This is a very long title that exceeds the maximum display width for sure"},
	}
	got := formatWorkItemLabel(item)
	if len([]rune(got)) > 40 {
		t.Errorf("expected label to be ≤40 chars, got %d: %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected truncated label to end with '...', got %q", got)
	}
}

func TestFormatWorkItemLabel_NoSource(t *testing.T) {
	item := daemonstate.WorkItem{ID: "item-xyz"}
	got := formatWorkItemLabel(item)
	if got != "item-xyz" {
		t.Errorf("expected 'item-xyz', got %q", got)
	}
}
