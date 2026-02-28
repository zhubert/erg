package workflow

import (
	"strings"
	"testing"
)

func TestGenerateMermaid_Default(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	out := GenerateMermaid(cfg)

	// Should contain basic transitions
	mustContain := []string{
		"stateDiagram-v2",
		"[*] --> coding",
		"coding -->",
		"open_pr -->",
		"await_review -->",
		"await_ci -->",
		"merge -->",
		"done --> [*]",
		"failed --> [*]",
	}

	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q\n\nFull output:\n%s", s, out)
		}
	}
}

func TestGenerateMermaid_WithHooks(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	cfg.States["coding"].After = []HookConfig{{Run: "echo test"}}

	out := GenerateMermaid(cfg)

	if !strings.Contains(out, "coding_hooks") {
		t.Errorf("output missing hook state\n\nFull output:\n%s", out)
	}
}

func TestGenerateMermaid_ErrorEdges(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	out := GenerateMermaid(cfg)

	// Coding should have error edge to failed
	if !strings.Contains(out, "coding --> failed : error") {
		t.Errorf("output missing error edge from coding\n\nFull output:\n%s", out)
	}
}

func TestGenerateMermaid_WaitTimeout(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	out := GenerateMermaid(cfg)

	// await_ci should show timeout
	if !strings.Contains(out, "timeout:") {
		t.Errorf("output missing timeout label\n\nFull output:\n%s", out)
	}
}

func TestGenerateMermaid_CustomProvider(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	cfg.Source.Provider = "linear"

	out := GenerateMermaid(cfg)
	// Just verify it still generates valid output
	if !strings.Contains(out, "stateDiagram-v2") {
		t.Errorf("expected valid mermaid output\n\nFull output:\n%s", out)
	}
}

func TestGenerateMermaidCompact_NoErrorEdges(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	out := GenerateMermaidCompact(cfg)

	if strings.Contains(out, ": error") {
		t.Errorf("compact output should not contain error edges\n\nFull output:\n%s", out)
	}
}

func TestGenerateMermaidCompact_NoHookNodes(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	cfg.States["coding"].Before = []HookConfig{{Run: "echo before"}}
	cfg.States["coding"].After = []HookConfig{{Run: "echo after"}}

	out := GenerateMermaidCompact(cfg)

	if strings.Contains(out, "coding_before") || strings.Contains(out, "coding_hooks") {
		t.Errorf("compact output should not contain hook pseudo-nodes\n\nFull output:\n%s", out)
	}
}

func TestGenerateMermaidCompact_HappyPath(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	out := GenerateMermaidCompact(cfg)

	mustContain := []string{
		"stateDiagram-v2",
		"[*] --> coding",
		"coding -->",
		"open_pr -->",
		"await_ci -->",
		"merge -->",
		"done --> [*]",
	}

	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("compact output missing %q\n\nFull output:\n%s", s, out)
		}
	}
}

func TestGenerateMermaidCompact_PreservesTimeouts(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	out := GenerateMermaidCompact(cfg)

	// Timeout transitions should still appear
	if !strings.Contains(out, "timeout") {
		t.Errorf("compact output should still contain timeout transitions\n\nFull output:\n%s", out)
	}
}

func TestGenerateMermaidCompact_PreservesChoices(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	out := GenerateMermaidCompact(cfg)

	// Choice transitions should still appear (check_ci_result is the choice state in the default workflow)
	if !strings.Contains(out, "check_ci_result -->") {
		t.Errorf("compact output should still contain choice transitions\n\nFull output:\n%s", out)
	}
}
