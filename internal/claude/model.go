package claude

// KnownModels is the set of all valid model identifiers — both shorthand
// aliases and canonical Anthropic model IDs. Used for config validation.
var KnownModels = map[string]bool{
	// Shorthand aliases
	"opus":   true,
	"sonnet": true,
	"haiku":  true,
	// Canonical model IDs
	"claude-opus-4-6":           true,
	"claude-sonnet-4-6":         true,
	"claude-haiku-4-5-20251001": true,
}

// modelAliases maps shorthand names to canonical Anthropic model IDs.
var modelAliases = map[string]string{
	"opus":   "claude-opus-4-6",
	"sonnet": "claude-sonnet-4-6",
	"haiku":  "claude-haiku-4-5-20251001",
}

// ResolveModel translates a shorthand alias to its canonical model ID.
// If the input is already a full model ID or empty, it is returned unchanged.
func ResolveModel(model string) string {
	if resolved, ok := modelAliases[model]; ok {
		return resolved
	}
	return model
}
