package claude

import "strings"

// modelAliases maps shorthand names to canonical Anthropic model IDs.
var modelAliases = map[string]string{
	"opus":   "claude-opus-4-6",
	"sonnet": "claude-sonnet-4-6",
	"haiku":  "claude-haiku-4-5-20251001",
}

// IsValidModel reports whether model is a recognized alias or looks like a
// canonical Claude model ID (any string starting with "claude-"). This avoids
// hard-coding every canonical ID while still catching obvious typos.
func IsValidModel(model string) bool {
	if _, ok := modelAliases[model]; ok {
		return true
	}
	return strings.HasPrefix(model, "claude-")
}

// ResolveModel translates a shorthand alias to its canonical model ID.
// If the input is already a full model ID or empty, it is returned unchanged.
func ResolveModel(model string) string {
	if resolved, ok := modelAliases[model]; ok {
		return resolved
	}
	return model
}
