package security

import "regexp"

var scrubPatterns = []*regexp.Regexp{
	// Generic key=value patterns.
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|credentials?)\s*[:=]\s*\S+`),
	// Anthropic API keys.
	regexp.MustCompile(`sk-ant-[a-zA-Z0-9\-_]{20,}`),
	// OpenAI API keys.
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),
	// GitHub tokens.
	regexp.MustCompile(`gh[pousr]_[a-zA-Z0-9]{36,}`),
	// AWS keys.
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// Generic long hex/base64 secrets (40+ chars after a known prefix).
	regexp.MustCompile(`(?i)(bearer|authorization)\s+\S{20,}`),
}

const redacted = "[REDACTED]"

// Scrub replaces potential secrets in text with [REDACTED].
func Scrub(text string) string {
	for _, p := range scrubPatterns {
		text = p.ReplaceAllString(text, redacted)
	}
	return text
}
