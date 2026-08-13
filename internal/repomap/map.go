package repomap

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// SkipDirs are directory names never indexed.
var SkipDirs = map[string]bool{
	".git": true, "node_modules": true, ".agent": true, "vendor": true,
	"target": true, "__pycache__": true, "dist": true, ".bounty": true,
}

// FileNode is one indexed source file.
type FileNode struct {
	Path    string   // root-relative, slash-separated
	Symbols []Symbol // capped
	Deps    []string // internal dependency targets (capped)
}

// DefaultMaxFiles and DefaultMaxRunes bound the rendered repo map.
const (
	DefaultMaxFiles = 300
	DefaultMaxRunes = 7000 // ≈ 1750 tokens at 4 runes/token (P6 裁剪：全量注入成本实测高于收益)
	MaxSymbolsFile  = 40
	MaxDepsFile     = 5
)

// Manager builds and caches a repository overview and refreshes it only when
// the fingerprint (paths + sizes + mtimes of indexed files) changes.
type Manager struct {
	mu          sync.Mutex
	root        string
	maxFiles    int
	maxRunes    int
	fingerprint string
	rendered    string
	built       bool
}

// NewManager creates a repo-map manager for root.
func NewManager(root string) *Manager {
	return &Manager{root: root, maxFiles: DefaultMaxFiles, maxRunes: DefaultMaxRunes}
}

// Render returns the current repo map block, building it on first use.
func (m *Manager) Render() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.built {
		m.rebuildLocked()
	}
	return m.rendered
}

// Refresh rebuilds only when the fingerprint changed since the last build.
// It returns the block and whether a rebuild happened.
func (m *Manager) Refresh() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fp := fingerprint(m.root)
	if m.built && fp == m.fingerprint {
		return m.rendered, false
	}
	m.fingerprint = fp
	m.rebuildLocked()
	return m.rendered, true
}

// ForceRender always rebuilds and returns the block (used by the repo_map
// tool so mid-task file changes become visible immediately).
func (m *Manager) ForceRender() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fingerprint = fingerprint(m.root)
	m.rebuildLocked()
	return m.rendered
}

func (m *Manager) rebuildLocked() {
	files := collectFiles(m.root, m.maxFiles)
	module := modulePath(filepath.Join(m.root, "go.mod"))
	topDirs := topDirNames(files)
	nodes := make([]FileNode, 0, len(files))
	totalSymbols := 0
	for _, p := range files {
		syms := IndexFile(p, "all", MaxSymbolsFile)
		totalSymbols += len(syms)
		deps := internalDeps(p, module, topDirs, MaxDepsFile)
		rel, _ := filepath.Rel(m.root, p)
		nodes = append(nodes, FileNode{Path: filepath.ToSlash(rel), Symbols: syms, Deps: deps})
	}
	m.rendered = renderBlock(nodes, totalSymbols, m.maxRunes)
	m.built = true
}

// collectFiles walks root and gathers supported source files, skipping SkipDirs.
func collectFiles(root string, maxFiles int) []string {
	var files []string
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			if p != root && SkipDirs[fi.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if SupportedExt(p) {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	if maxFiles > 0 && len(files) > maxFiles {
		files = files[:maxFiles]
	}
	return files
}

// fingerprint hashes (relative path | size | mtime) for every supported file.
func fingerprint(root string) string {
	h := sha256.New()
	for _, p := range collectFiles(root, 0) {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(root, p)
		fmt.Fprintf(h, "%s|%d|%d\n", filepath.ToSlash(rel), fi.Size(), fi.ModTime().UnixNano())
	}
	return fmt.Sprintf("%x", h.Sum(nil)[:8])
}

// modulePath reads the module declaration from go.mod, if present.
func modulePath(goMod string) string {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// topDirNames returns the set of first-level path segments seen in files.
func topDirNames(files []string) map[string]bool {
	out := map[string]bool{}
	for _, p := range files {
		if idx := strings.IndexByte(p, filepath.Separator); idx > 0 {
			out[p[:idx]] = true
		}
	}
	return out
}

var (
	goImportRe = regexp.MustCompile(`import\s+(?:[A-Za-z0-9_]+\s+)?"([^"]+)"`)
	pyImportRe = regexp.MustCompile(`^\s*(?:from\s+([\w.]+)\s+import|import\s+([\w.]+))`)
	tsImportRe = regexp.MustCompile(`(?:from\s+|import\()\s*['"]([^'"]+)['"]`)
	tsReqRe    = regexp.MustCompile(`require\(\s*['"]([^'"]+)['"]`)
)

// internalDeps extracts import targets that point inside the repository
// (Go module path, same-top-directory Python modules, or relative TS imports).
func internalDeps(path, module string, topDirs map[string]bool, max int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(data)
	ext := strings.ToLower(filepath.Ext(path))

	var raw []string
	switch ext {
	case ".go":
		for _, m := range goImportRe.FindAllStringSubmatch(text, -1) {
			raw = append(raw, m[1])
		}
		raw = goBlockImports(text, raw)
	case ".py":
		for _, m := range pyImportRe.FindAllStringSubmatch(text, -1) {
			v := m[1]
			if v == "" {
				v = m[2]
			}
			raw = append(raw, v)
		}
	case ".ts", ".tsx", ".js":
		for _, m := range tsImportRe.FindAllStringSubmatch(text, -1) {
			raw = append(raw, m[1])
		}
		for _, m := range tsReqRe.FindAllStringSubmatch(text, -1) {
			raw = append(raw, m[1])
		}
	}

	seen := map[string]bool{}
	var out []string
	for _, dep := range raw {
		dep = strings.TrimSpace(dep)
		if dep == "" || seen[dep] {
			continue
		}
		internal := false
		if strings.HasPrefix(dep, ".") || strings.HasPrefix(dep, "/") {
			internal = true
		} else if module != "" && (dep == module || strings.HasPrefix(dep, module+"/")) {
			internal = true
		} else if seg := firstSegment(dep); seg != "" && topDirs[seg] {
			internal = true
		}
		if internal {
			seen[dep] = true
			out = append(out, dep)
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

// goBlockImports appends imports found inside `import ( ... )` blocks.
func goBlockImports(text string, raw []string) []string {
	lines := strings.Split(text, "\n")
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "import (") {
			inBlock = true
			continue
		}
		if inBlock {
			if trimmed == ")" {
				inBlock = false
				continue
			}
			if i := strings.Index(trimmed, `"`); i >= 0 {
				rest := trimmed[i+1:]
				if j := strings.Index(rest, `"`); j >= 0 {
					raw = append(raw, rest[:j])
				}
			}
		}
	}
	return raw
}

func firstSegment(dep string) string {
	dep = strings.TrimPrefix(dep, "@")
	for _, sep := range []string{"/", "."} {
		if idx := strings.Index(dep, sep); idx > 0 {
			return dep[:idx]
		}
	}
	return dep
}

// renderBlock formats the repo map, truncating to maxRunes.
func renderBlock(nodes []FileNode, totalSymbols, maxRunes int) string {
	if len(nodes) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Repo Map\n")
	sb.WriteString(fmt.Sprintf("<!-- files=%d symbols=%d -->\n", len(nodes), totalSymbols))
	prevDir := ""
	for _, n := range nodes {
		dir := pathDir(n.Path)
		if dir != prevDir {
			prevDir = dir
			sb.WriteString(fmt.Sprintf("\n### %s/\n", dir))
		}
		base := filepath.Base(n.Path)
		sb.WriteString(fmt.Sprintf("- %s (%d symbols)\n", base, len(n.Symbols)))
		for _, s := range n.Symbols {
			sb.WriteString(fmt.Sprintf("    [%s] %s (L%d)\n", s.Kind, s.Name, s.Line))
		}
		if len(n.Deps) > 0 {
			sb.WriteString("    ↳ deps: " + strings.Join(n.Deps, ", ") + "\n")
		}
	}
	out := sb.String()
	if runes := []rune(out); len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "\n... [repo map truncated]"
	}
	return out
}

func pathDir(p string) string {
	if idx := strings.LastIndex(p, "/"); idx > 0 {
		return p[:idx]
	}
	return "."
}
