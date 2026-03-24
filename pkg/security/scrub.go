package security

import "regexp"

type scrubRule struct {
	re      *regexp.Regexp
	replace string
}

var scrubPatterns = []scrubRule{
	// Generic key=value patterns.
	{regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|credentials?)\s*[:=]\s*\S+`), "${1}=[REDACTED]"},
	// Anthropic API keys.
	{regexp.MustCompile(`sk-ant-[a-zA-Z0-9\-_]{20,}`), "[REDACTED]"},
	// OpenAI API keys.
	{regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`), "[REDACTED]"},
	// GitHub tokens.
	{regexp.MustCompile(`gh[pousr]_[a-zA-Z0-9]{36,}`), "[REDACTED]"},
	// AWS keys.
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), "[REDACTED]"},
	// Generic long hex/base64 secrets (40+ chars after a known prefix).
	{regexp.MustCompile(`(?i)(bearer|authorization)\s+\S{20,}`), "${1} [REDACTED]"},
	// JWT tokens.
	{regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`), "[REDACTED]"},
	// Slack tokens.
	{regexp.MustCompile(`xox[baprs]-[a-zA-Z0-9\-]{10,}`), "[REDACTED]"},
	// Private keys.
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), "[REDACTED]"},
	// Connection strings with passwords.
	{regexp.MustCompile(`(?i)(://[^:]+:)[^@]+(@)`), "${1}[REDACTED]${2}"},
}

// Scrub replaces potential secrets in text with [REDACTED].
func Scrub(text string) string {
	for _, p := range scrubPatterns {
		text = p.re.ReplaceAllString(text, p.replace)
	}
	return text
}
