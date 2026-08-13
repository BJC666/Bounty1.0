package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// FileSnap records the pre-edit state of a file.
type FileSnap struct {
	Path     string `json:"path"`
	Content  string `json:"content,omitempty"` // empty if file didn't exist (delete on restore)
	Existed  bool   `json:"existed"`           // false = file was created this turn
	Encoding string `json:"encoding,omitempty"`
}

// TurnCheckpoint captures the state at the start of a turn.
type TurnCheckpoint struct {
	Turn     int        `json:"turn"`
	Prompt   string     `json:"prompt"`
	MsgIndex int        `json:"msg_index"` // index in session.Messages at turn start
	Files    []FileSnap `json:"files"`
}

// Store manages checkpoints for one session.
type Store struct {
	mu   sync.Mutex
	dir  string
	turn int
	seen map[string]bool // paths already captured this turn
}

// New creates or opens a checkpoint store for a session.
func New(sessionDir string) (*Store, error) {
	ckptDir := filepath.Join(sessionDir, ".checkpoints")
	if err := os.MkdirAll(ckptDir, 0755); err != nil {
		return nil, err
	}
	return &Store{dir: ckptDir, seen: make(map[string]bool)}, nil
}

// BeginTurn starts a new turn. Call at the start of each user turn.
func (s *Store) BeginTurn(prompt string, msgIndex int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turn++
	s.seen = make(map[string]bool) // reset per-turn tracking
}

// GetTurn returns the current turn number.
func (s *Store) GetTurn() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turn
}

// Capture records a file's pre-edit state. Call BEFORE edit/write operations.
// Only the first touch of each path per turn is recorded.
func (s *Store) Capture(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if s.seen[absPath] {
		return nil
	}
	s.seen[absPath] = true

	snap := FileSnap{Path: absPath}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			snap.Existed = false
		} else {
			return err
		}
	} else {
		snap.Existed = true
		snap.Content = string(data)
	}

	return s.saveSnapshot(snap)
}

func (s *Store) saveSnapshot(snap FileSnap) error {
	path := filepath.Join(s.dir, fmt.Sprintf("turn-%d.json", s.turn))

	// Read existing or create new
	var ckpt TurnCheckpoint
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &ckpt)
	}
	ckpt.Turn = s.turn
	ckpt.Files = append(ckpt.Files, snap)

	newData, err := json.MarshalIndent(ckpt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, newData, 0644)
}

// SaveTurn persists the turn checkpoint with metadata.
func (s *Store) SaveTurn(prompt string, msgIndex int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, fmt.Sprintf("turn-%d.json", s.turn))
	var ckpt TurnCheckpoint
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &ckpt)
	}
	ckpt.Turn = s.turn
	ckpt.Prompt = prompt
	ckpt.MsgIndex = msgIndex

	newData, err := json.MarshalIndent(ckpt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, newData, 0644)
}

// List returns all turn checkpoints for picker UI.
func (s *Store) List() ([]TurnCheckpoint, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var checkpoints []TurnCheckpoint
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var ckpt TurnCheckpoint
		if json.Unmarshal(data, &ckpt) == nil {
			checkpoints = append(checkpoints, ckpt)
		}
	}
	return checkpoints, nil
}

// Restore reverts files to their state at the given turn.
func (s *Store) Restore(turn int) error {
	path := filepath.Join(s.dir, fmt.Sprintf("turn-%d.json", turn))
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("checkpoint for turn %d not found: %w", turn, err)
	}

	var ckpt TurnCheckpoint
	if err := json.Unmarshal(data, &ckpt); err != nil {
		return err
	}

	for _, f := range ckpt.Files {
		if !f.Existed {
			os.Remove(f.Path) // file was created this turn → delete
		} else {
			if err := os.WriteFile(f.Path, []byte(f.Content), 0644); err != nil {
				return fmt.Errorf("restore %s: %w", f.Path, err)
			}
		}
	}
	return nil
}

// GetMsgIndex returns the message index for a turn (for session truncation).
func (s *Store) GetMsgIndex(turn int) (int, error) {
	path := filepath.Join(s.dir, fmt.Sprintf("turn-%d.json", turn))
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var ckpt TurnCheckpoint
	if err := json.Unmarshal(data, &ckpt); err != nil {
		return 0, err
	}
	return ckpt.MsgIndex, nil
}

// ListCheckpoints maps legacy turn checkpoints to message-indexed Info for the
// same picker UI GitStore feeds.
func (s *Store) ListCheckpoints() ([]Info, error) {
	ckpts, err := s.List()
	if err != nil {
		return nil, err
	}
	list := make([]Info, 0, len(ckpts))
	for _, c := range ckpts {
		list = append(list, Info{MsgIndex: c.MsgIndex, Prompt: c.Prompt})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].MsgIndex < list[j].MsgIndex })
	return list, nil
}

// RestoreCheckpoint finds the legacy turn whose MsgIndex matches and restores
// its file snapshots.
func (s *Store) RestoreCheckpoint(msgIndex int) error {
	ckpts, err := s.List()
	if err != nil {
		return err
	}
	for _, c := range ckpts {
		if c.MsgIndex == msgIndex {
			return s.Restore(c.Turn)
		}
	}
	return fmt.Errorf("checkpoint for message %d not found", msgIndex)
}
