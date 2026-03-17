package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhubert/erg/internal/claude"
	"github.com/zhubert/erg/internal/daemonstate"
	"github.com/zhubert/erg/internal/exec"
	"github.com/zhubert/erg/internal/git"
	"github.com/zhubert/erg/internal/issues"
	"github.com/zhubert/erg/internal/session"
	"github.com/zhubert/erg/internal/worker"
	"github.com/zhubert/erg/internal/workflow"
)

// newIntegrationDaemon creates a daemon wired for integration testing with a real
// tick() loop. It returns the daemon, mock executor, and fake provider.
func newIntegrationDaemon(t *testing.T, mockExec *exec.MockExecutor) (*Daemon, *issues.FakeProvider) {
	t.Helper()

	cfg := testConfig()
	cfg.Repos = []string{"/test/repo"}

	// Set FilePath so saveConfig succeeds (writes to a temp file).
	tmpFile := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(tmpFile, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.SetFilePath(tmpFile)

	gitSvc := git.NewGitServiceWithExecutor(mockExec)
	sessSvc := session.NewSessionServiceWithExecutor(mockExec)
	logger := discardLogger()

	fakeProvider := issues.NewFakeProvider(issues.SourceGitHub)
	registry := issues.NewProviderRegistry(fakeProvider)

	d := New(cfg, gitSvc, sessSvc, registry, logger)
	d.sessionMgr.SetSkipMessageLoad(true)
	d.state = daemonstate.NewDaemonState("/test/repo")
	d.dockerHealthCheck = func(context.Context) error { return nil }
	d.repoFilter = "/test/repo"
	d.autoMerge = true

	// Install workflow
	wfCfg := workflow.DefaultWorkflowConfig()
	d.workflowConfigs = map[string]*workflow.Config{"/test/repo": wfCfg}
	reg := d.buildActionRegistry()
	checker := newEventChecker(d)
	d.engines = map[string]*workflow.Engine{
		"/test/repo": workflow.NewEngine(wfCfg, reg, checker, d.logger),
	}

	// Zero out time-gated operations so they fire on first tick.
	d.lastReviewPollAt = time.Time{}
	d.lastReconcileAt = time.Time{}

	return d, fakeProvider
}

// addBaseGitMocks sets up the MockExecutor rules needed for GitHub polling
// and session creation (the startCoding path).
func addBaseGitMocks(t *testing.T, mockExec *exec.MockExecutor, ghIssues []git.GitHubIssue) {
	t.Helper()

	issueJSON, _ := json.Marshal(ghIssues)

	// GitHub polling: gh issue list
	mockExec.AddPrefixMatch("gh", []string{"issue", "list"}, exec.MockResponse{
		Stdout: issueJSON,
	})

	// git remote get-url origin (needed by repoFilter matching and PR creation)
	mockExec.AddExactMatch("git", []string{"remote", "get-url", "origin"}, exec.MockResponse{
		Stdout: []byte("git@github.com:owner/repo.git\n"),
	})

	// GetDefaultBranch: git symbolic-ref
	mockExec.AddExactMatch("git", []string{"symbolic-ref", "refs/remotes/origin/HEAD"}, exec.MockResponse{
		Stdout: []byte("refs/remotes/origin/main\n"),
	})

	// FetchOrigin: git fetch origin
	mockExec.AddPrefixMatch("git", []string{"fetch", "origin"}, exec.MockResponse{})

	// rev-parse --verify origin/main (session creation — verify remote ref exists)
	mockExec.AddExactMatch("git", []string{"rev-parse", "--verify", "origin/main"}, exec.MockResponse{
		Stdout: []byte("abc123\n"),
	})

	// rev-parse --verify main (GetDefaultBranch fallback)
	mockExec.AddExactMatch("git", []string{"rev-parse", "--verify", "main"}, exec.MockResponse{
		Stdout: []byte("abc123\n"),
	})

	// BranchExists: rev-parse --verify for issue branches must FAIL so
	// startCoding creates a new branch instead of trying to resume.
	// This catch-all returns an error for all other rev-parse --verify calls.
	mockExec.AddRule(func(dir, name string, args []string) bool {
		return name == "git" && len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--verify"
	}, exec.MockResponse{Err: fmt.Errorf("fatal: Needed a single revision")})

	// git worktree add (session creation)
	mockExec.AddPrefixMatch("git", []string{"worktree", "add"}, exec.MockResponse{
		Stdout: []byte("Preparing worktree\n"),
	})

	// git rev-parse --abbrev-ref HEAD (getCurrentBranchName)
	mockExec.AddExactMatch("git", []string{"rev-parse", "--abbrev-ref", "HEAD"}, exec.MockResponse{
		Stdout: []byte("main\n"),
	})

	// GetLinkedPRsForIssue (pre-flight check during polling)
	mockExec.AddPrefixMatch("gh", []string{"api", "graphql"}, exec.MockResponse{
		Stdout: mockGitHubGraphQL(nil),
	})

	// GetPRForBranch (idempotent PR check in startCoding): no existing PR
	// Use "open" specifically to avoid catching "all" used by GetBatchPRStatesWithComments.
	mockExec.AddPrefixMatch("gh", []string{"pr", "list", "--state", "open"}, exec.MockResponse{
		Stdout: []byte("[]"),
	})

	// git -C ... log (branch divergence check — empty means no divergence)
	mockExec.AddPrefixMatch("git", []string{"-C"}, exec.MockResponse{
		Stdout: []byte(""),
	})
}

// addPRCreateMocks adds MockExecutor rules for the open_pr action (push + create PR).
func addPRCreateMocks(t *testing.T, mockExec *exec.MockExecutor, prURL string) {
	t.Helper()

	// git status --porcelain (EnsureCommitted → GetWorktreeStatus: no uncommitted changes)
	mockExec.AddExactMatch("git", []string{"status", "--porcelain"}, exec.MockResponse{
		Stdout: []byte(""),
	})

	// git push -u origin <branch>
	mockExec.AddPrefixMatch("git", []string{"push"}, exec.MockResponse{
		Stdout: []byte("Everything up-to-date\n"),
	})

	// gh pr create (the PR title/body generation may fail, fallback to --fill)
	mockExec.AddPrefixMatch("gh", []string{"pr", "create"}, exec.MockResponse{
		Stdout: []byte(prURL + "\n"),
	})

	// branchHasChanges: git rev-list --count (check if branch has commits ahead)
	// Note: branchHasChanges uses os/exec directly, so we can't mock it here.
	// The CreatePR path does its own EnsureCommitted which uses the executor.
	// The branchHasChanges call uses os/exec.CommandContext (not the executor),
	// so it will fail in tests — but createPR catches the error and proceeds.

	// GeneratePRTitleAndBodyWithIssueRef (Claude call — will fail, falls back to --fill)
	// This is handled by the gh pr create prefix match above.
}

// addCIAndReviewMocks adds MockExecutor rules that handle both CI checks and
// review approval. Because MockExecutor uses first-match-wins ordering and
// rules persist across ticks, we install a single set of smart matchers that
// respond correctly to the specific --json field requested.
func addCIAndReviewMocks(t *testing.T, mockExec *exec.MockExecutor) {
	t.Helper()

	// gh pr view <branch> --json <fields>
	// Different callers request different fields:
	//   - CheckPRMergeableStatus: --json mergeable
	//   - GetPRState:             --json state
	//   - CheckPRReviewDecision:  --json reviews
	mockExec.AddRule(func(dir, name string, args []string) bool {
		return name == "gh" && len(args) >= 4 && args[0] == "pr" && args[1] == "view" && args[3] == "--json"
	}, exec.MockResponse{
		// Return a superset JSON object that satisfies all callers
		Stdout: []byte(`{"state":"OPEN","mergeable":"MERGEABLE","reviews":[{"author":{"login":"reviewer"},"state":"APPROVED"}]}`),
	})

	// CheckPRChecks: gh pr checks <branch> --json name,state
	mockExec.AddPrefixMatch("gh", []string{"pr", "checks"}, exec.MockResponse{
		Stdout: []byte(`[{"name":"ci","state":"SUCCESS"}]`),
	})

	// GetBatchPRStatesWithComments: gh pr list --state all --json ...
	mockExec.AddPrefixMatch("gh", []string{"pr", "list", "--state", "all"}, exec.MockResponse{
		Stdout: []byte(`[{"state":"OPEN","headRefName":"issue-42","comments":[],"reviews":[{"body":"lgtm","state":"APPROVED"}]}]`),
	})

	// gh pr merge <branch> --rebase
	mockExec.AddPrefixMatch("gh", []string{"pr", "merge"}, exec.MockResponse{})
}

// completeWorker replaces the real worker with an already-done worker.
func completeWorker(t *testing.T, d *Daemon, itemID string) {
	t.Helper()
	d.mu.Lock()
	d.workers[itemID] = worker.NewDoneWorker()
	d.mu.Unlock()
}

// completeWorkerWithError replaces the real worker with an already-done-with-error worker.
func completeWorkerWithError(t *testing.T, d *Daemon, itemID string, err error) {
	t.Helper()
	d.mu.Lock()
	d.workers[itemID] = worker.NewDoneWorkerWithError(err)
	d.mu.Unlock()
}

// installMockRunnerFactory installs a runner factory that creates MockRunners
// which complete immediately (no real Claude process).
func installMockRunnerFactory(t *testing.T, d *Daemon) {
	t.Helper()
	d.sessionMgr.SetRunnerFactory(func(sessionID, workingDir, repoPath string, sessionStarted bool, initialMessages []claude.Message) claude.RunnerInterface {
		r := claude.NewMockRunner(sessionID, sessionStarted, initialMessages)
		r.QueueResponse(claude.ResponseChunk{
			Content: "Done implementing changes.",
			Done:    true,
		})
		r.CompleteStreaming("Done implementing changes.")
		return r
	})
}

// --- Integration Tests ---

func TestIntegration_HappyPath_IssueToMerge(t *testing.T) {
	mockExec := exec.NewMockExecutor(nil)
	prURL := "https://github.com/owner/repo/pull/10"

	addBaseGitMocks(t, mockExec, []git.GitHubIssue{
		{Number: 42, Title: "Fix bug", Body: "Please fix the bug", URL: "https://github.com/owner/repo/issues/42"},
	})

	d, _ := newIntegrationDaemon(t, mockExec)
	installMockRunnerFactory(t, d)
	ctx := context.Background()

	// --- Tick 1: Poll → queue → start coding ---
	d.tick(ctx)

	// Verify: 1 work item exists, active, step=coding, phase=async_pending
	items := d.state.GetActiveWorkItems()
	if len(items) != 1 {
		t.Fatalf("tick 1: expected 1 active item, got %d", len(items))
	}
	item := items[0]
	if item.CurrentStep != "coding" {
		t.Errorf("tick 1: expected step=coding, got %s", item.CurrentStep)
	}
	if item.Phase != "async_pending" {
		t.Errorf("tick 1: expected phase=async_pending, got %s", item.Phase)
	}
	d.mu.Lock()
	workerCount := len(d.workers)
	d.mu.Unlock()
	if workerCount != 1 {
		t.Errorf("tick 1: expected 1 worker, got %d", workerCount)
	}
	itemID := item.ID

	// --- Simulate worker completion ---
	completeWorker(t, d, itemID)

	// Add mocks for PR creation (open_pr action)
	addPRCreateMocks(t, mockExec, prURL)

	// --- Tick 2: Worker collected → sync chain runs open_pr → await_ci ---
	d.tick(ctx)

	item2, ok := d.state.GetWorkItem(itemID)
	if !ok {
		t.Fatal("tick 2: work item not found")
	}
	if item2.CurrentStep != "await_ci" {
		t.Errorf("tick 2: expected step=await_ci, got %s", item2.CurrentStep)
	}
	if item2.Phase != "idle" {
		t.Errorf("tick 2: expected phase=idle, got %s", item2.Phase)
	}
	if item2.PRURL == "" {
		t.Error("tick 2: expected PRURL to be set")
	}

	// --- Add CI + review + merge mocks ---
	// Install once: smart matchers handle all phases (CI, review, merge)
	addCIAndReviewMocks(t, mockExec)

	// --- Tick 3: CI check fires → advances through check_ci_result → await_review ---
	d.tick(ctx)

	item3, ok := d.state.GetWorkItem(itemID)
	if !ok {
		t.Fatal("tick 3: work item not found")
	}
	// check_ci_result is a choice state that should route to await_review when ci_passed=true
	if item3.CurrentStep != "await_review" {
		t.Errorf("tick 3: expected step=await_review, got %s", item3.CurrentStep)
	}
	if item3.Phase != "idle" {
		t.Errorf("tick 3: expected phase=idle, got %s", item3.Phase)
	}

	// --- Tick 4: Review fires → merge → done ---
	d.lastReviewPollAt = time.Time{} // Force review poll to run
	d.tick(ctx)

	item4, ok := d.state.GetWorkItem(itemID)
	if !ok {
		t.Fatal("tick 4: work item not found")
	}
	if item4.CurrentStep != "done" {
		t.Errorf("tick 4: expected step=done, got %s", item4.CurrentStep)
	}
	if item4.State != daemonstate.WorkItemCompleted {
		t.Errorf("tick 4: expected state=completed, got %s", item4.State)
	}
}

func TestIntegration_WorkerFailure_FollowsErrorEdge(t *testing.T) {
	mockExec := exec.NewMockExecutor(nil)

	addBaseGitMocks(t, mockExec, []git.GitHubIssue{
		{Number: 99, Title: "Flaky feature", URL: "https://github.com/owner/repo/issues/99"},
	})

	d, _ := newIntegrationDaemon(t, mockExec)
	installMockRunnerFactory(t, d)
	ctx := context.Background()

	// Tick 1: Poll → queue → start coding
	d.tick(ctx)

	items := d.state.GetActiveWorkItems()
	if len(items) != 1 {
		t.Fatalf("tick 1: expected 1 active item, got %d", len(items))
	}
	itemID := items[0].ID

	// Simulate worker completion with error
	completeWorkerWithError(t, d, itemID, errors.New("API error: rate limited"))

	// Tick 2: Worker collected with error → follows error edge → failed
	d.tick(ctx)

	item, ok := d.state.GetWorkItem(itemID)
	if !ok {
		t.Fatal("tick 2: work item not found")
	}
	if item.CurrentStep != "failed" {
		t.Errorf("tick 2: expected step=failed, got %s", item.CurrentStep)
	}
	if item.State != daemonstate.WorkItemFailed {
		t.Errorf("tick 2: expected state=failed, got %s", item.State)
	}
}

func TestIntegration_MaxConcurrent_Respected(t *testing.T) {
	mockExec := exec.NewMockExecutor(nil)

	addBaseGitMocks(t, mockExec, []git.GitHubIssue{
		{Number: 1, Title: "Issue 1", URL: "https://github.com/owner/repo/issues/1"},
		{Number: 2, Title: "Issue 2", URL: "https://github.com/owner/repo/issues/2"},
		{Number: 3, Title: "Issue 3", URL: "https://github.com/owner/repo/issues/3"},
	})

	d, _ := newIntegrationDaemon(t, mockExec)
	installMockRunnerFactory(t, d)
	d.maxConcurrent = 1
	ctx := context.Background()

	// Tick 1: Should poll and pick up 1 issue (concurrency limit)
	d.tick(ctx)

	active := d.state.GetActiveWorkItems()
	queued := d.state.GetWorkItemsByState(daemonstate.WorkItemQueued)

	// With maxConcurrent=1, pollForNewIssues checks:
	//   activeSlots + queuedCount >= maxConcurrent
	// After first issue is queued (queuedCount=1, activeSlots=0), it stops polling.
	// Then startQueuedItems activates the queued item (activeSlots=1).
	totalItems := len(active) + len(queued)
	if totalItems != 1 {
		t.Errorf("tick 1: expected 1 total item (active+queued), got %d (active=%d, queued=%d)",
			totalItems, len(active), len(queued))
	}
	if len(active) != 1 {
		t.Errorf("tick 1: expected 1 active item, got %d", len(active))
	}
}

func TestIntegration_Deduplication(t *testing.T) {
	mockExec := exec.NewMockExecutor(nil)

	addBaseGitMocks(t, mockExec, []git.GitHubIssue{
		{Number: 42, Title: "Fix bug", URL: "https://github.com/owner/repo/issues/42"},
	})

	d, _ := newIntegrationDaemon(t, mockExec)
	installMockRunnerFactory(t, d)
	ctx := context.Background()

	// Tick 1: Pick up issue #42
	d.tick(ctx)

	all1 := d.state.GetAllWorkItems()
	if len(all1) != 1 {
		t.Fatalf("tick 1: expected 1 work item, got %d", len(all1))
	}

	// Tick 2: gh issue list still returns #42 — should not create duplicate
	d.tick(ctx)

	all2 := d.state.GetAllWorkItems()
	if len(all2) != 1 {
		t.Errorf("tick 2: expected 1 work item (deduplicated), got %d", len(all2))
	}
}

func TestIntegration_ExternalClose_CancelsWorkItem(t *testing.T) {
	mockExec := exec.NewMockExecutor(nil)

	addBaseGitMocks(t, mockExec, []git.GitHubIssue{
		{Number: 42, Title: "Fix bug", URL: "https://github.com/owner/repo/issues/42"},
	})

	d, fakeProvider := newIntegrationDaemon(t, mockExec)
	installMockRunnerFactory(t, d)
	ctx := context.Background()

	// Tick 1: Pick up issue, start coding
	d.tick(ctx)

	items := d.state.GetActiveWorkItems()
	if len(items) != 1 {
		t.Fatalf("tick 1: expected 1 active item, got %d", len(items))
	}
	itemID := items[0].ID

	// Complete the worker so the item moves to a wait state
	completeWorker(t, d, itemID)

	// Add PR create mocks
	prURL := "https://github.com/owner/repo/pull/5"
	addPRCreateMocks(t, mockExec, prURL)

	// Tick 2: open_pr → await_ci
	d.tick(ctx)

	item2, ok := d.state.GetWorkItem(itemID)
	if !ok {
		t.Fatal("tick 2: work item not found")
	}
	if item2.CurrentStep != "await_ci" {
		t.Fatalf("tick 2: expected step=await_ci, got %s", item2.CurrentStep)
	}

	// Simulate external issue closure via the FakeProvider
	fakeProvider.SetIssueClosed("42", true)

	// Also need to mock gh issue view for GetIssueState fallback
	mockExec.AddExactMatch("gh", []string{"issue", "view", "42", "--json", "state"}, exec.MockResponse{
		Stdout: []byte(`{"state":"CLOSED"}`),
	})

	// Add a mock for the gh issue comment (unqueue comment)
	mockExec.AddPrefixMatch("gh", []string{"issue", "comment"}, exec.MockResponse{})

	// Zero out reconcile time so it fires immediately
	d.lastReconcileAt = time.Time{}

	// Tick 3: reconcileClosedIssues detects closure → marks failed
	d.tick(ctx)

	item3, ok := d.state.GetWorkItem(itemID)
	if !ok {
		t.Fatal("tick 3: work item not found")
	}
	if item3.State != daemonstate.WorkItemFailed {
		t.Errorf("tick 3: expected state=failed, got %s", item3.State)
	}

	if item3.ErrorMessage == "" {
		t.Error("tick 3: expected error message to be set")
	}
}

// --- Fake provider tests ---

func TestFakeProvider_ImplementsAllInterfaces(t *testing.T) {
	fp := issues.NewFakeProvider(issues.SourceGitHub)

	// Test Provider interface
	if fp.Name() != "Fake-github" {
		t.Errorf("expected name Fake-github, got %s", fp.Name())
	}
	if fp.Source() != issues.SourceGitHub {
		t.Errorf("expected source github, got %s", fp.Source())
	}
	if !fp.IsConfigured("/any/repo") {
		t.Error("expected IsConfigured=true by default")
	}

	issue := issues.Issue{ID: "42", Title: "Test", Source: issues.SourceGitHub}
	if branch := fp.GenerateBranchName(issue); branch != "issue-42" {
		t.Errorf("expected branch issue-42, got %s", branch)
	}
	if link := fp.GetPRLinkText(issue); link != "Fixes #42" {
		t.Errorf("expected link text Fixes #42, got %s", link)
	}
}

func TestFakeProvider_FetchIssues(t *testing.T) {
	fp := issues.NewFakeProvider(issues.SourceGitHub)
	ctx := context.Background()

	// Empty by default
	result, err := fp.FetchIssues(ctx, "/repo", issues.FilterConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 issues, got %d", len(result))
	}

	// Set issues
	fp.SetIssues([]issues.Issue{
		{ID: "1", Title: "First"},
		{ID: "2", Title: "Second"},
	})
	result, err = fp.FetchIssues(ctx, "/repo", issues.FilterConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 issues, got %d", len(result))
	}

	// Set error
	fp.SetFetchError(fmt.Errorf("network error"))
	_, err = fp.FetchIssues(ctx, "/repo", issues.FilterConfig{})
	if err == nil {
		t.Error("expected error")
	}
}

func TestFakeProvider_CallRecording(t *testing.T) {
	fp := issues.NewFakeProvider(issues.SourceGitHub)
	ctx := context.Background()

	_ = fp.Comment(ctx, "/repo", "42", "Hello world")
	_ = fp.RemoveLabel(ctx, "/repo", "42", "ai-assisted")

	if len(fp.CommentCalls) != 1 {
		t.Fatalf("expected 1 comment call, got %d", len(fp.CommentCalls))
	}
	if fp.CommentCalls[0].IssueID != "42" {
		t.Errorf("expected issue 42, got %s", fp.CommentCalls[0].IssueID)
	}
	if fp.CommentCalls[0].Args[0] != "Hello world" {
		t.Errorf("expected body 'Hello world', got %s", fp.CommentCalls[0].Args[0])
	}

	if len(fp.RemoveLabelCalls) != 1 {
		t.Fatalf("expected 1 remove-label call, got %d", len(fp.RemoveLabelCalls))
	}
	if fp.RemoveLabelCalls[0].Args[0] != "ai-assisted" {
		t.Errorf("expected label 'ai-assisted', got %s", fp.RemoveLabelCalls[0].Args[0])
	}
}

func TestFakeProvider_IssueStateChecker(t *testing.T) {
	fp := issues.NewFakeProvider(issues.SourceGitHub)
	ctx := context.Background()

	closed, err := fp.IsIssueClosed(ctx, "/repo", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if closed {
		t.Error("expected not closed by default")
	}

	fp.SetIssueClosed("42", true)
	closed, err = fp.IsIssueClosed(ctx, "/repo", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !closed {
		t.Error("expected closed after SetIssueClosed")
	}
}
