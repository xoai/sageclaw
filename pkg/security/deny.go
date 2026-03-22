package security

import (
	"fmt"
	"regexp"
)

// ErrDeniedCommand is returned when a command matches a deny pattern.
var ErrDeniedCommand = fmt.Errorf("command denied by security policy")

var denyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`rm\s+(-[a-zA-Z]*f[a-zA-Z]*\s+)?/`),
	regexp.MustCompile(`mkfs`),
	regexp.MustCompile(`(?i)format\s+[a-zA-Z]:`),
	regexp.MustCompile(`dd\s+if=`),
	regexp.MustCompile(`:\(\)\s*\{`),        // fork bomb
	regexp.MustCompile(`(?i)shutdown`),
	regexp.MustCompile(`(?i)reboot`),
	regexp.MustCompile(`init\s+[06]`),
	regexp.MustCompile(`chmod\s+777\s+/`),    // wide-open root perms
	regexp.MustCompile(`>\s*/dev/sd[a-z]`),   // overwrite block device
	regexp.MustCompile(`mv\s+/`),             // move root-level paths
}

// CheckCommand returns an error if the command matches a deny pattern.
func CheckCommand(cmd string) error {
	for _, p := range denyPatterns {
		if p.MatchString(cmd) {
			return fmt.Errorf("%w: matches pattern %s", ErrDeniedCommand, p.String())
		}
	}
	return nil
}
