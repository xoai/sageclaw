package security

import (
	"fmt"
	"regexp"
)

// ErrDeniedCommand is returned when a command matches a deny pattern.
var ErrDeniedCommand = fmt.Errorf("command denied by security policy")

// DenyGroup is a named set of deny patterns for shell commands.
type DenyGroup struct {
	Name     string
	Patterns []*regexp.Regexp
}

// AllDenyGroups defines all available deny pattern groups.
// All are enabled by default.
var AllDenyGroups = map[string]DenyGroup{
	"destructive": {
		Name: "destructive",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`rm\s+(-[a-zA-Z]*f[a-zA-Z]*\s+)?/`),
			regexp.MustCompile(`mkfs`),
			regexp.MustCompile(`dd\s+if=`),
			regexp.MustCompile(`(?i)shutdown`),
			regexp.MustCompile(`(?i)reboot`),
			regexp.MustCompile(`init\s+[06]`),
			regexp.MustCompile(`:\(\)\s*\{`), // fork bomb
		},
	},
	"dangerous_paths": {
		Name: "dangerous_paths",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`chmod\s+777\s+/`),
			regexp.MustCompile(`>\s*/dev/sd[a-z]`),
			regexp.MustCompile(`mv\s+/`),
			regexp.MustCompile(`(?i)format\s+[a-zA-Z]:`),
		},
	},
	"data_exfiltration": {
		Name: "data_exfiltration",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`curl\s+.*\|\s*sh`),
			regexp.MustCompile(`wget\s+.*\|\s*sh`),
			regexp.MustCompile(`curl\s+.*(-X\s+(POST|PUT)|--data)`),
			regexp.MustCompile(`/dev/tcp/`),
		},
	},
	"reverse_shell": {
		Name: "reverse_shell",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`\bnc\b.*-e`),
			regexp.MustCompile(`\bncat\b`),
			regexp.MustCompile(`\bsocat\b`),
			regexp.MustCompile(`openssl\s+s_client`),
			regexp.MustCompile(`\btelnet\b`),
			regexp.MustCompile(`\bmkfifo\b`),
		},
	},
	"privilege_escalation": {
		Name: "privilege_escalation",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`\bsudo\b`),
			regexp.MustCompile(`\bsu\s+-`),
			regexp.MustCompile(`\bnsenter\b`),
			regexp.MustCompile(`\bmount\b`),
		},
	},
	"env_injection": {
		Name: "env_injection",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`LD_PRELOAD=`),
			regexp.MustCompile(`DYLD_INSERT_LIBRARIES=`),
			regexp.MustCompile(`LD_LIBRARY_PATH=`),
			regexp.MustCompile(`BASH_ENV=`),
		},
	},
	"code_injection": {
		Name: "code_injection",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`eval\s+\$`),
			regexp.MustCompile(`base64\s+.*-d.*\|\s*sh`),
		},
	},
	"env_dump": {
		Name: "env_dump",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`\bprintenv\b`),
			regexp.MustCompile(`(?m)\benv\b\s*$`),
			regexp.MustCompile(`/proc/[^/]+/environ`),
		},
	},
	"network_recon": {
		Name: "network_recon",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`\bnmap\b`),
			regexp.MustCompile(`\bmasscan\b`),
			regexp.MustCompile(`\bssh\b.*@`),
		},
	},
	"container_escape": {
		Name: "container_escape",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`docker\.sock`),
			regexp.MustCompile(`/proc/sys/`),
			regexp.MustCompile(`/sys/kernel/`),
		},
	},
	"crypto_mining": {
		Name: "crypto_mining",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`\bxmrig\b`),
			regexp.MustCompile(`\bminerd\b`),
			regexp.MustCompile(`\bcpuminer\b`),
			regexp.MustCompile(`stratum\+tcp://`),
			regexp.MustCompile(`\bcgminer\b`),
			regexp.MustCompile(`\bbfgminer\b`),
		},
	},
	"filter_bypass": {
		Name: "filter_bypass",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`\$\(.*\)\s*\|\s*(sh|bash)`), // command substitution piped to shell
			regexp.MustCompile("`.+`\\s*\\|\\s*(sh|bash)"),  // backtick substitution piped to shell
			regexp.MustCompile(`\$\{.*#.*\}`),               // parameter manipulation
			regexp.MustCompile(`\\x[0-9a-fA-F]{2}`),        // hex encoding
			regexp.MustCompile(`\$'\\.+'`),                  // ANSI-C quoting
			regexp.MustCompile(`\bxxd\b.*\|\s*(sh|bash)`),   // hex decode to shell
		},
	},
	"package_install": {
		Name: "package_install",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`\bapt(-get)?\s+install\b`),
			regexp.MustCompile(`\byum\s+install\b`),
			regexp.MustCompile(`\bdnf\s+install\b`),
			regexp.MustCompile(`\bpacman\s+-S\b`),
			regexp.MustCompile(`\bbrew\s+install\b`),
			regexp.MustCompile(`\bsnap\s+install\b`),
			regexp.MustCompile(`\bpip\s+install\b`),
			regexp.MustCompile(`\bnpm\s+install\s+-g\b`),
		},
	},
	"persistence": {
		Name: "persistence",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`\bcrontab\b`),
			regexp.MustCompile(`/etc/cron`),
			regexp.MustCompile(`\bsystemctl\s+(enable|start)\b`),
			regexp.MustCompile(`/etc/init\.d/`),
			regexp.MustCompile(`\.bashrc`),
			regexp.MustCompile(`\.bash_profile`),
			regexp.MustCompile(`\.profile`),
			regexp.MustCompile(`/etc/rc\.local`),
		},
	},
	"process_control": {
		Name: "process_control",
		Patterns: []*regexp.Regexp{
			regexp.MustCompile(`\bkill\s+-9\b`),
			regexp.MustCompile(`\bkillall\b`),
			regexp.MustCompile(`\bpkill\b`),
			regexp.MustCompile(`\bsignal\s+SIGKILL\b`),
		},
	},
}

// DenyGroupNames returns all available deny group names.
func DenyGroupNames() []string {
	names := make([]string, 0, len(AllDenyGroups))
	for name := range AllDenyGroups {
		names = append(names, name)
	}
	return names
}

// ResolveDenyPatterns collects all deny patterns from enabled groups.
// disabledGroups follows the same convention as CheckCommand: a group is
// disabled when its value is false (i.e., {"crypto_mining": false} disables it).
// Nil means all groups enabled.
func ResolveDenyPatterns(disabledGroups map[string]bool) []*regexp.Regexp {
	var patterns []*regexp.Regexp
	for groupName, group := range AllDenyGroups {
		if disabled, ok := disabledGroups[groupName]; ok && !disabled {
			continue
		}
		patterns = append(patterns, group.Patterns...)
	}
	return patterns
}

// CheckCommand returns an error if the command matches any enabled deny pattern.
// disabledGroups contains group names set to false to skip. Nil means all groups enabled.
func CheckCommand(cmd string, disabledGroups map[string]bool) error {
	for groupName, group := range AllDenyGroups {
		// Skip disabled groups.
		if disabled, ok := disabledGroups[groupName]; ok && !disabled {
			continue
		}
		for _, p := range group.Patterns {
			if p.MatchString(cmd) {
				return fmt.Errorf("%w: matches pattern %s (group: %s)", ErrDeniedCommand, p.String(), groupName)
			}
		}
	}
	return nil
}
