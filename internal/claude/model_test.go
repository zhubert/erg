package claude

import "testing"

func TestResolveModel_Aliases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"opus", "claude-opus-4-6"},
		{"sonnet", "claude-sonnet-4-6"},
		{"haiku", "claude-haiku-4-5-20251001"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ResolveModel(tt.input)
			if got != tt.want {
				t.Errorf("ResolveModel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveModel_FullIDPassthrough(t *testing.T) {
	fullID := "claude-sonnet-4-6"
	got := ResolveModel(fullID)
	if got != fullID {
		t.Errorf("ResolveModel(%q) = %q, want passthrough %q", fullID, got, fullID)
	}
}

func TestResolveModel_EmptyPassthrough(t *testing.T) {
	got := ResolveModel("")
	if got != "" {
		t.Errorf("ResolveModel(\"\") = %q, want empty string", got)
	}
}

func TestResolveModel_UnknownPassthrough(t *testing.T) {
	got := ResolveModel("gpt-4o")
	if got != "gpt-4o" {
		t.Errorf("ResolveModel(\"gpt-4o\") = %q, want passthrough \"gpt-4o\"", got)
	}
}

func TestKnownModels_ContainsAliases(t *testing.T) {
	aliases := []string{"opus", "sonnet", "haiku"}
	for _, alias := range aliases {
		if !KnownModels[alias] {
			t.Errorf("KnownModels[%q] should be true", alias)
		}
	}
}

func TestKnownModels_ContainsCanonicalIDs(t *testing.T) {
	ids := []string{
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
	}
	for _, id := range ids {
		if !KnownModels[id] {
			t.Errorf("KnownModels[%q] should be true", id)
		}
	}
}

func TestKnownModels_UnknownIsFalse(t *testing.T) {
	unknowns := []string{"gpt-4o", "gemini-pro", "llama-3", ""}
	for _, u := range unknowns {
		if KnownModels[u] {
			t.Errorf("KnownModels[%q] should be false", u)
		}
	}
}

func TestBuildCommandArgs_Model(t *testing.T) {
	config := ProcessConfig{
		SessionID:     "session-with-model",
		WorkingDir:    "/tmp",
		MCPConfigPath: "/tmp/mcp.json",
		AllowedTools:  []string{"Read"},
		Model:         "claude-haiku-4-5-20251001",
	}

	args := BuildCommandArgs(config)

	modelVal := getArgValue(args, "--model")
	if modelVal != "claude-haiku-4-5-20251001" {
		t.Errorf("expected --model claude-haiku-4-5-20251001, got %q", modelVal)
	}
}

func TestBuildCommandArgs_NoModel(t *testing.T) {
	config := ProcessConfig{
		SessionID:     "session-no-model",
		WorkingDir:    "/tmp",
		MCPConfigPath: "/tmp/mcp.json",
		AllowedTools:  []string{"Read"},
	}

	args := BuildCommandArgs(config)

	for i, arg := range args {
		if arg == "--model" {
			t.Errorf("unexpected --model flag at position %d with value %q", i, args[i+1])
		}
	}
}
