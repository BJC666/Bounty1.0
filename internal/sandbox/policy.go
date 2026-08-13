package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Policy pre-checks a shell command before execution: outbound-network tools
// are blocked when Network is off, redirection targets must stay inside the
// workspace (or the allow list), and forbid-read/forbid-write path patterns
// apply to every path-like token in the command.
type Policy struct {
	workspace   string
	allowWrite  []string
	forbidRead  []string
	forbidWrite []string
	network     bool
}

func NewPolicy(workspace string, allowWrite, forbidRead, forbidWrite []string, network bool) *Policy {
	return &Policy{
		workspace:   workspace,
		allowWrite:  allowWrite,
		forbidRead:  forbidRead,
		forbidWrite: forbidWrite,
		network:     network,
	}
}

var outboundPatterns = []string{
	"curl", "wget", "invoke-webrequest", "invoke-restmethod", "iwr ",
	"pip install", "python -m pip install", "npm install", "npm i ",
	"pnpm install", "yarn add", "git clone http", "git clone https",
	"start http", "bitsadmin", "certutil -urlcache",
	"new-object system.net.webclient", "downloadfile", "downloadstring",
}

var pathTokenRe = regexp.MustCompile(`["']([A-Za-z]:[\\/][^"']+|/(?:[A-Za-z0-9_.-]+/)+[^"']*)["']|([A-Za-z]:[\\/][^\s"'|&<>]+|/(?:[A-Za-z0-9_.-]+/)+[^\s"'|&<>]*|\.[\\/][^\s"'|&<>]+)`)

// Check inspects command and returns the first policy violation found.
func (p *Policy) Check(command string) error {
	lower := strings.ToLower(command)

	if !p.network {
		for _, pat := range outboundPatterns {
			if strings.Contains(lower, pat) {
				return fmt.Errorf("沙箱网络策略：network=false 禁止出站网络，命令包含 %q 被拦截", pat)
			}
		}
	}

	for _, target := range redirectionTargets(command) {
		// Windows null device: `> nul` discards output — harmless, not a
		// filesystem write. Whitelisted so sandbox false-positives do not
		// poison legitimate command idioms.
		if strings.EqualFold(strings.Trim(target, `"`), "nul") ||
			strings.EqualFold(strings.Trim(target, `"`), `\.\nul`) {
			continue
		}
		abs, ok := absolutePathLocal(target)
		if !ok {
			continue
		}
		if matchesAny(p.forbidWrite, abs) {
			return fmt.Errorf("沙箱写策略：%s 命中 forbid_write", abs)
		}
		if !p.inWorkspaceOrAllowed(abs) {
			return fmt.Errorf("沙箱写策略：禁止写入 %s（workspace 之外且不在允许列表）", abs)
		}
	}

	for _, m := range pathTokenRe.FindAllStringSubmatch(command, -1) {
		tok := strings.Trim(m[1], `"'`)
		if tok == "" {
			tok = strings.Trim(m[2], `"'`)
		}
		abs, ok := absolutePathLocal(tok)
		if !ok {
			continue
		}
		if matchesAny(p.forbidWrite, abs) {
			return fmt.Errorf("沙箱写策略：%s 命中 forbid_write", abs)
		}
		if matchesAny(p.forbidRead, abs) {
			return fmt.Errorf("沙箱读策略：%s 命中 forbid_read", abs)
		}
	}
	return nil
}

// inWorkspaceOrAllowed reports whether abs is inside the workspace or
// covered by an allow-write pattern.
func (p *Policy) inWorkspaceOrAllowed(abs string) bool {
	if p.workspace != "" {
		ws := filepath.Clean(p.workspace)
		if abs == ws || strings.HasPrefix(abs, ws+string(filepath.Separator)) {
			return true
		}
	}
	return matchesAny(p.allowWrite, abs)
}

// redirectionTargets extracts the target token of every shell output
// redirection (`>`, `>>`, `2>`, `1>`, `&>`) that appears outside quotes, so
// quoted content like echo "5 > 3" is not mistaken for a redirection.
func redirectionTargets(command string) []string {
	var out []string
	inSingle, inDouble := false, false
	i, n := 0, len(command)
	for i < n {
		c := command[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			i++
		case c == '"' && !inSingle:
			inDouble = !inDouble
			i++
		case inSingle || inDouble:
			i++
		case c == '>':
			// Consume any additional `>` and leading digits (2> / 1>>).
			j := i
			for j < n && (command[j] == '>' || (command[j] >= '0' && command[j] <= '9')) {
				j++
			}
			for j < n && (command[j] == ' ' || command[j] == '\t') {
				j++
			}
			start := j
			for j < n && !strings.ContainsRune(" \t|&<>;", rune(command[j])) {
				if command[j] == '"' || command[j] == '\'' {
					quote := command[j]
					j++
					for j < n && command[j] != quote {
						j++
					}
					if j < n {
						j++
					}
					continue
				}
				j++
			}
			if tok := strings.Trim(command[start:j], `"'`); tok != "" {
				out = append(out, tok)
			}
			i = j
		default:
			i++
		}
	}
	return out
}

// absolutePathLocal resolves a path to absolute, cleaned form (symlinks
// resolved when possible). Mirrors permission.absolutePath.
func absolutePathLocal(p string) (string, bool) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return abs, true
}

// matchesAny mirrors permission.matchesPolicy: absolute, relative and
// home-relative glob patterns, case-insensitive on Windows, and a bare
// directory forbids its whole subtree.
func matchesAny(patterns []string, p string) bool {
	for _, pattern := range patterns {
		if matchesPolicyLocal(pattern, p) {
			return true
		}
	}
	return false
}

func matchesPolicyLocal(pattern, p string) bool {
	if strings.HasPrefix(pattern, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			pattern = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(pattern, "~"), string(filepath.Separator)))
		}
	}
	pattern = filepath.FromSlash(pattern)
	if pattern == "" {
		return false
	}
	segs := strings.Split(p, string(filepath.Separator))
	for i := range segs {
		cand := strings.Join(segs[i:], string(filepath.Separator))
		if matchPolicyOneLocal(pattern, cand) {
			return true
		}
	}
	return false
}

func matchPolicyOneLocal(pattern, cand string) bool {
	if runtime.GOOS == "windows" {
		pattern = strings.ToLower(pattern)
		cand = strings.ToLower(cand)
	}
	if ok, _ := filepath.Match(pattern, cand); ok {
		return true
	}
	dir := strings.TrimSuffix(pattern, "*")
	dir = strings.TrimRight(dir, `/\`)
	if dir != "" && dir != "." {
		if cand == dir || strings.HasPrefix(cand, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
