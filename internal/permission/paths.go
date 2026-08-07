package permission

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// expandHome replaces a leading "~" in p with the current user's home
// directory. Paths without a leading "~" are returned unchanged.
func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~"+string(filepath.Separator)) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// absolutePath resolves a user-supplied path to its absolute, cleaned form.
// Symlinks are resolved when possible so that a linked path cannot bypass a
// policy. The bool result is false when the path cannot be resolved, in which
// case callers should not attempt the operation.
func absolutePath(p string) (string, bool) {
	abs, err := filepath.Abs(expandHome(p))
	if err != nil {
		return "", false
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return abs, true
}

// matchesPolicy reports whether a forbid pattern applies to the path p.
// Patterns may be absolute ("/etc/*", "C:\Windows\*"), relative
// ("Windows/*", "System32/*") or home-relative ("~/.ssh/*"). Relative
// patterns are matched against every trailing segment suffix of p, so
// "Windows/*" protects C:\Windows and everything below it. A trailing "*"
// (or a bare directory name) forbids the whole subtree below that directory.
// Matching is case-insensitive on Windows.
func matchesPolicy(pattern, p string) bool {
	pattern = filepath.FromSlash(expandHome(pattern))
	if pattern == "" {
		return false
	}
	segs := strings.Split(p, string(filepath.Separator))
	for i := range segs {
		cand := strings.Join(segs[i:], string(filepath.Separator))
		if matchPolicyOne(pattern, cand) {
			return true
		}
	}
	return false
}

func matchPolicyOne(pattern, cand string) bool {
	if runtime.GOOS == "windows" {
		pattern = strings.ToLower(pattern)
		cand = strings.ToLower(cand)
	}
	if ok, _ := filepath.Match(pattern, cand); ok {
		return true
	}
	// "dir/*" (or a bare "dir") forbids the whole subtree below dir.
	dir := strings.TrimSuffix(pattern, "*")
	dir = strings.TrimRight(dir, `/\`)
	if dir != "" && dir != "." {
		if cand == dir || strings.HasPrefix(cand, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
