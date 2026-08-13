package checkpoint

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Info describes one restorable checkpoint in a frontend-friendly shape.
// MsgIndex is the session message index of the user message the checkpoint was
// taken before; rolling back to it restores the workspace to the state at the
// start of that message.
type Info struct {
	MsgIndex int    `json:"msg_index"`
	Prompt   string `json:"prompt"`
}

// Restorer rolls the workspace back to a previous user message (file level).
// Implemented by GitStore (shadow git repo) and by the legacy file Store.
type Restorer interface {
	ListCheckpoints() ([]Info, error)
	RestoreCheckpoint(msgIndex int) error
}

// gitCfg pins deterministic git behavior regardless of the user's global
// config: byte-exact round trips (no autocrlf/eol rewriting), long paths on
// Windows, and no quoting of non-ASCII paths in output.
var gitCfg = []string{
	"-c", "core.autocrlf=false",
	"-c", "core.eol=lf",
	"-c", "core.safecrlf=false",
	"-c", "core.longpaths=true",
	"-c", "core.quotepath=false",
}

// GitStore implements the agent.Checkpointer contract with a shadow bare git
// repository: every user message commits a full workspace snapshot and tags it
// msg-<N>. Because the snapshot is a complete tree (including ignored files,
// via git add -f), rollback covers edits by any tool and cleans up untracked
// files, which the legacy file Store could not do.
type GitStore struct {
	mu      sync.Mutex
	wsRoot  string // absolute workspace root being snapshotted
	repo    string // bare shadow repo dir (kept outside the workspace)
	index   string // index file inside repo (GIT_INDEX_FILE)
	metaDir string // per-message prompt files
	turn    int
}

// NewGit creates a GitStore. dir is the storage directory (session-scoped,
// outside the workspace); the bare repo lives at dir/shadow.git. It returns an
// error when git is missing or the workspace root is unusable so the caller
// can fall back to the legacy file Store.
func NewGit(wsRoot, dir string) (*GitStore, error) {
	if strings.TrimSpace(wsRoot) == "" {
		return nil, fmt.Errorf("workspace root is empty")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git not found on PATH: %w", err)
	}
	absRoot, err := filepath.Abs(wsRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	repo := filepath.Join(dir, "shadow.git")
	s := &GitStore{
		wsRoot:  absRoot,
		repo:    repo,
		index:   filepath.Join(repo, "index"),
		metaDir: filepath.Join(dir, "prompts"),
	}
	if err := os.MkdirAll(s.metaDir, 0755); err != nil {
		return nil, err
	}
	cmd := exec.Command("git", append(append([]string{}, gitCfg...), "init", "--bare", "-q", repo)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git init --bare: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return s, nil
}

// BeginTurn increments the turn counter. The actual snapshot commit happens in
// SaveTurn, which is called immediately afterwards by the agent.
func (s *GitStore) BeginTurn(prompt string, msgIndex int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turn++
}

// Capture is a no-op: GitStore snapshots the whole tree per message, so it does
// not need per-file pre-edit captures.
func (s *GitStore) Capture(path string) error {
	return nil
}

// GetTurn returns the number of turns seen so far.
func (s *GitStore) GetTurn() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turn
}

// SaveTurn commits the current workspace tree and tags it msg-<msgIndex>, and
// persists the prompt for the picker UI.
func (s *GitStore) SaveTurn(prompt string, msgIndex int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.commitAndTag(msgIndex); err != nil {
		return fmt.Errorf("checkpoint msg-%d: %w", msgIndex, err)
	}
	if err := os.WriteFile(s.promptFile(msgIndex), []byte(prompt), 0644); err != nil {
		return fmt.Errorf("checkpoint msg-%d prompt: %w", msgIndex, err)
	}
	return nil
}

// RestoreCheckpoint rewinds the workspace to the tag msg-<msgIndex>: the index
// is reset to the tag tree, files not in that tree are deleted (bottom-up,
// skipping .git), then every tracked file is force-written back.
func (s *GitStore) RestoreCheckpoint(msgIndex int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tag := fmt.Sprintf("msg-%d", msgIndex)
	tracked, err := s.listTree(tag)
	if err != nil {
		return fmt.Errorf("checkpoint %s not restorable: %w", tag, err)
	}
	if err := s.removeExtras(tracked); err != nil {
		return fmt.Errorf("cleanup before restore: %w", err)
	}
	if err := s.run("read-tree", tag); err != nil {
		return fmt.Errorf("read-tree %s: %w", tag, err)
	}
	if err := s.run("checkout-index", "-a", "-f"); err != nil {
		return fmt.Errorf("checkout-index: %w", err)
	}
	// Move HEAD so later commits build on the restored state (non-fatal:
	// tags are what rollback reads, not branch tips).
	if ref := s.headRef(); ref != "" {
		_ = s.run("update-ref", ref, tag)
	}
	return nil
}

// ListCheckpoints returns all msg-<N> tags ordered by message index.
func (s *GitStore) ListCheckpoints() ([]Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out, err := s.output("tag", "-l", "msg-*")
	if err != nil {
		return nil, err
	}
	list := []Info{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(line)
		if !strings.HasPrefix(name, "msg-") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, "msg-"))
		if err != nil {
			continue
		}
		prompt, _ := os.ReadFile(s.promptFile(n))
		list = append(list, Info{MsgIndex: n, Prompt: string(prompt)})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].MsgIndex < list[j].MsgIndex })
	return list, nil
}

func (s *GitStore) promptFile(msgIndex int) string {
	return filepath.Join(s.metaDir, fmt.Sprintf("msg-%d.txt", msgIndex))
}

// commitAndTag adds the full workspace (forcing ignored files in so the
// snapshot is complete), commits with --allow-empty, and (re)tags msg-<N>.
func (s *GitStore) commitAndTag(msgIndex int) error {
	tag := fmt.Sprintf("msg-%d", msgIndex)

	args := []string{"add", "-A", "-f", "--", "."}
	if rel := s.shadowRel(); rel != "" {
		args = append(args, ":(exclude)"+rel)
	}
	args = append(args, ":(exclude).git")
	if err := s.run(args...); err != nil {
		return err
	}

	commitArgs := []string{"-c", "user.name=Bounty Checkpoint", "-c", "user.email=checkpoint@bounty.local",
		"commit", "-q", "--allow-empty", "-m", "checkpoint " + tag}
	if err := s.run(commitArgs...); err != nil {
		return err
	}
	if err := s.run("tag", "-f", tag); err != nil {
		return err
	}
	return nil
}

// shadowRel returns the shadow repo path relative to the workspace when the
// repo sits inside the workspace (unusual BOUNTY_HOME layout), so snapshots
// never self-nest. Empty when outside.
func (s *GitStore) shadowRel() string {
	rel, err := filepath.Rel(s.wsRoot, s.repo)
	if err != nil {
		return ""
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

// listTree returns the set of file paths present in a tag tree.
func (s *GitStore) listTree(tag string) (map[string]bool, error) {
	out, err := s.output("ls-tree", "-r", "-z", "--name-only", tag)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, name := range bytes.Split(out, []byte{0}) {
		if len(name) == 0 {
			continue
		}
		set[string(name)] = true
	}
	return set, nil
}

// removeExtras deletes workspace entries that are not in the tag tree,
// deepest-first so empty directories collapse. The workspace's own .git
// directory and (if applicable) the shadow repo are never touched.
func (s *GitStore) removeExtras(tracked map[string]bool) error {
	skip := map[string]bool{".git": true}
	if rel := s.shadowRel(); rel != "" {
		skip[strings.SplitN(rel, "/", 2)[0]] = true
	}

	type entry struct {
		path  string
		rel   string
		isDir bool
	}
	var entries []entry
	err := filepath.Walk(s.wsRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(s.wsRoot, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if skip[first] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entries = append(entries, entry{path: p, rel: filepath.ToSlash(rel), isDir: info.IsDir()})
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if tracked[e.rel] {
			continue
		}
		if e.isDir {
			// Removing a non-empty dir fails; that is fine — tracked
			// children keep it alive.
			_ = os.Remove(e.path)
			continue
		}
		if err := os.Remove(e.path); err != nil {
			return err
		}
	}
	return nil
}

// headRef returns the shadow repo's current branch ref, or "" on failure.
func (s *GitStore) headRef() string {
	out, err := s.output("symbolic-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// run executes a git command with the shadow repo environment and returns the
// combined output on failure.
func (s *GitStore) run(args ...string) error {
	cmd := exec.Command("git", append(append([]string{}, gitCfg...), args...)...)
	cmd.Dir = s.wsRoot
	cmd.Env = s.gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// output runs a git command and returns stdout.
func (s *GitStore) output(args ...string) ([]byte, error) {
	cmd := exec.Command("git", append(append([]string{}, gitCfg...), args...)...)
	cmd.Dir = s.wsRoot
	cmd.Env = s.gitEnv()
	return cmd.Output()
}

// gitEnv points git at the shadow repo without touching the workspace's own
// repository state: GIT_DIR selects the bare repo, GIT_WORK_TREE the workspace,
// and GIT_INDEX_FILE a private index.
func (s *GitStore) gitEnv() []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+3)
	for _, kv := range base {
		switch {
		case strings.HasPrefix(kv, "GIT_DIR="),
			strings.HasPrefix(kv, "GIT_WORK_TREE="),
			strings.HasPrefix(kv, "GIT_INDEX_FILE="):
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"GIT_DIR="+s.repo,
		"GIT_WORK_TREE="+s.wsRoot,
		"GIT_INDEX_FILE="+s.index,
	)
}
