package agent

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/zhubert/plural-core/config"
	"github.com/zhubert/plural-core/exec"
	"github.com/zhubert/plural-core/git"
	"github.com/zhubert/plural-core/issues"
	"github.com/zhubert/plural-core/session"

	"github.com/zhubert/plural-agent/internal/worker"
)

func TestCheckReviewApproval(t *testing.T) {
	// The mock executor doesn't have gh CLI rules, so CheckPRReviewDecision
	// will fail, returning ReviewNone by default. We test different attempt values.
	t.Run("early attempt continues polling", func(t *testing.T) {
		cfg := testConfig()
		a := testAgent(cfg)

		sess := testSession("merge-review-1")
		cfg.AddSession(*sess)

		// With failed git call, reviewDecision defaults to "" which maps to ReviewNone.
		// At early attempts, ReviewNone should continue polling.
		action := worker.CheckReviewApproval(a, "merge-review-1", sess, 1)
		if action != worker.MergeActionContinue {
			t.Errorf("expected worker.MergeActionContinue at attempt 1, got %d", action)
		}
	})

	t.Run("max attempts stops polling", func(t *testing.T) {
		cfg := testConfig()
		a := testAgent(cfg)

		sess := testSession("merge-review-2")
		cfg.AddSession(*sess)

		action := worker.CheckReviewApproval(a, "merge-review-2", sess, 120)
		if action != worker.MergeActionStop {
			t.Errorf("expected worker.MergeActionStop at max attempts, got %d", action)
		}
	})
}

func TestCheckCIAndMerge(t *testing.T) {
	t.Run("empty mock executor returns CIStatusPassing and proceeds to merge", func(t *testing.T) {
		cfg := testConfig()
		a := testAgent(cfg)

		sess := testSession("merge-ci-1")
		cfg.AddSession(*sess)

		// The default mock executor returns empty success for all commands.
		// CheckPRChecks interprets exit 0 with unparseable output as CIStatusPassing.
		// CIStatusPassing triggers doMerge, which also "succeeds" with mock executor.
		action := worker.CheckCIAndMerge(a, "merge-ci-1", sess, 1)
		if action != worker.MergeActionProceed {
			t.Errorf("expected worker.MergeActionProceed (mock returns passing CI), got %d", action)
		}
	})

	t.Run("with failing CI mock returns mergeActionStop", func(t *testing.T) {
		cfg := testConfig()
		mockExec := exec.NewMockExecutor(nil)
		// Mock gh pr checks to return failing status
		mockExec.AddRule(
			func(dir, name string, args []string) bool {
				return name == "gh" && len(args) >= 2 && args[0] == "pr" && args[1] == "checks"
			},
			exec.MockResponse{
				Stdout: []byte(`[{"state":"FAILURE"}]`),
				Err:    fmt.Errorf("exit status 1"),
			},
		)
		gitSvc := git.NewGitServiceWithExecutor(mockExec)
		sessSvc := session.NewSessionServiceWithExecutor(mockExec)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		registry := issues.NewProviderRegistry()

		a := New(cfg, gitSvc, sessSvc, registry, logger)
		a.sessionMgr.SetSkipMessageLoad(true)

		sess := testSession("merge-ci-fail")
		cfg.AddSession(*sess)

		action := worker.CheckCIAndMerge(a, "merge-ci-fail", sess, 1)
		if action != worker.MergeActionStop {
			t.Errorf("expected worker.MergeActionStop for failing CI, got %d", action)
		}
	})

	t.Run("with pending CI mock returns mergeActionContinue", func(t *testing.T) {
		cfg := testConfig()
		mockExec := exec.NewMockExecutor(nil)
		// Mock gh pr checks to return pending status
		mockExec.AddRule(
			func(dir, name string, args []string) bool {
				return name == "gh" && len(args) >= 2 && args[0] == "pr" && args[1] == "checks"
			},
			exec.MockResponse{
				Stdout: []byte(`[{"state":"PENDING"}]`),
				Err:    fmt.Errorf("exit status 1"),
			},
		)
		gitSvc := git.NewGitServiceWithExecutor(mockExec)
		sessSvc := session.NewSessionServiceWithExecutor(mockExec)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		registry := issues.NewProviderRegistry()

		a := New(cfg, gitSvc, sessSvc, registry, logger)
		a.sessionMgr.SetSkipMessageLoad(true)

		sess := testSession("merge-ci-pending")
		cfg.AddSession(*sess)

		action := worker.CheckCIAndMerge(a, "merge-ci-pending", sess, 1)
		if action != worker.MergeActionContinue {
			t.Errorf("expected worker.MergeActionContinue for pending CI, got %d", action)
		}
	})
}

func TestCheckAndAddressComments_NoNewComments(t *testing.T) {
	cfg := testConfig()
	a := testAgent(cfg)

	sess := testSession("merge-comments-1")
	sess.PRCommentsAddressedCount = 0
	cfg.AddSession(*sess)

	// With mock executor, GetBatchPRStatesWithComments will fail,
	// which causes it to proceed (not block on comment check failure).
	action := worker.CheckAndAddressComments(a, "merge-comments-1", sess, 1)
	if action != worker.MergeActionProceed {
		t.Errorf("expected worker.MergeActionProceed when comment check fails, got %d", action)
	}
}

func TestMergeActionConstants(t *testing.T) {
	// Verify the merge action constants are distinct and have expected values
	if worker.MergeActionContinue != 0 {
		t.Errorf("expected worker.MergeActionContinue=0, got %d", worker.MergeActionContinue)
	}
	if worker.MergeActionStop != 1 {
		t.Errorf("expected worker.MergeActionStop=1, got %d", worker.MergeActionStop)
	}
	if worker.MergeActionProceed != 2 {
		t.Errorf("expected worker.MergeActionProceed=2, got %d", worker.MergeActionProceed)
	}
}

func TestDoMerge_WithMockExecutor(t *testing.T) {
	t.Run("default mock succeeds", func(t *testing.T) {
		cfg := testConfig()
		a := testAgent(cfg)

		sess := &config.Session{
			ID:       "merge-test-1",
			RepoPath: "/test/repo",
			Branch:   "feature-x",
		}
		cfg.AddSession(*sess)

		// Default mock executor returns empty success for MergePR
		action := worker.DoMerge(a, "merge-test-1", sess)
		if action != worker.MergeActionProceed {
			t.Errorf("expected worker.MergeActionProceed on merge success, got %d", action)
		}

		// Verify session was marked as merged
		updated := cfg.GetSession("merge-test-1")
		if updated != nil && !updated.PRMerged {
			t.Error("expected session to be marked as PR merged")
		}
	})

	t.Run("merge error returns stop", func(t *testing.T) {
		cfg := testConfig()
		mockExec := exec.NewMockExecutor(nil)
		mockExec.AddRule(
			func(dir, name string, args []string) bool {
				return name == "gh" && len(args) >= 2 && args[0] == "pr" && args[1] == "merge"
			},
			exec.MockResponse{
				Err: fmt.Errorf("merge conflict"),
			},
		)
		gitSvc := git.NewGitServiceWithExecutor(mockExec)
		sessSvc := session.NewSessionServiceWithExecutor(mockExec)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		registry := issues.NewProviderRegistry()

		a := New(cfg, gitSvc, sessSvc, registry, logger)
		a.sessionMgr.SetSkipMessageLoad(true)

		sess := &config.Session{
			ID:       "merge-test-2",
			RepoPath: "/test/repo",
			Branch:   "feature-y",
		}
		cfg.AddSession(*sess)

		action := worker.DoMerge(a, "merge-test-2", sess)
		if action != worker.MergeActionStop {
			t.Errorf("expected worker.MergeActionStop on merge failure, got %d", action)
		}
	})
}
