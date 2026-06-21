package agent

import "strings"

// isContextWindowError classifies an LLM error as a context-window overflow.
//
// The previous implementation used single-keyword substring matching
// (e.g. strings.Contains(err, "token")), which false-positived on auth
// errors ("invalid token"), rate limits ("token budget"), and JSON parse
// errors ("unexpected length").
//
// This replacement uses compound patterns: a match requires BOTH a
// category keyword AND a contextual qualifier, dramatically reducing
// false positives while still catching all known provider error formats.
func isContextWindowError(errMsg string) bool {
	lower := strings.ToLower(errMsg)

	// ── Explicit exclusions ─────────────────────────────────────────
	// These patterns are definitely NOT context window errors, even if
	// they contain trigger words like "token".
	for _, excl := range contextErrorExclusions {
		if strings.Contains(lower, excl) {
			return false
		}
	}

	// ── Exact provider-specific patterns ────────────────────────────
	// These are unambiguous and don't need compound matching.
	for _, exact := range contextErrorExactPatterns {
		if strings.Contains(lower, exact) {
			return true
		}
	}

	// ── Compound patterns ───────────────────────────────────────────
	// Require BOTH a category keyword AND a qualifier.
	for _, cp := range contextErrorCompoundPatterns {
		if strings.Contains(lower, cp.keyword) {
			for _, q := range cp.qualifiers {
				if strings.Contains(lower, q) {
					return true
				}
			}
		}
	}

	return false
}

// contextErrorExclusions — if any of these appear, it is NOT a context error.
var contextErrorExclusions = []string{
	"invalid token",
	"token expired",
	"token revoked",
	"invalid api key",
	"invalid api_key",
	"unauthorized",
	"forbidden",
	"authentication",
	"401",
	"403",
	"too many requests",
	"rate limit",
	"quota exceeded",
	"quota exhausted",
	"429",
}

// contextErrorExactPatterns — unambiguous, single-match patterns.
var contextErrorExactPatterns = []string{
	"content_length_exceeded",      // Anthropic
	"context_length_exceeded",      // OpenAI
	"max_tokens",                   // OpenAI / generic
	"maximum context length",       // OpenAI
	"context window",               // Generic
	"reduce the length",            // OpenAI
	"reduce your prompt",           // OpenAI
	"payload too large",            // HTTP 413
	"request entity too large",     // HTTP 413
	"input too long",               // Various
	"prompt is too long",           // Various
	"total tokens exceeded",        // Various
	"token limit",                  // Various
	"context_length",               // Generic field name
	"maximum model length",         // Various
	"model's maximum context",      // Various
	"too many tokens",              // Various
	"exceeds the model's limit",    // Various
	"string too long",              // Vertex AI
}

// compoundPattern requires the keyword AND at least one qualifier.
type compoundPattern struct {
	keyword    string
	qualifiers []string
}

// contextErrorCompoundPatterns — require keyword + qualifier to match.
var contextErrorCompoundPatterns = []compoundPattern{
	{
		keyword:    "token",
		qualifiers: []string{"limit", "exceed", "maximum", "overflow", "too many", "too long", "reduce"},
	},
	{
		keyword:    "context",
		qualifiers: []string{"length", "exceed", "limit", "overflow", "window", "too long"},
	},
	{
		keyword:    "length",
		qualifiers: []string{"exceed", "limit", "maximum", "too long", "overflow", "invalid"},
	},
	{
		keyword:    "413",
		qualifiers: []string{"payload", "entity", "large", "request"},
	},
}
