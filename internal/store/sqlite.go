package store

import (
	"database/sql"
	_ "embed"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

type Message struct {
	ID        int64  `json:"id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls string `json:"tool_calls,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	Usage     string `json:"usage,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

type Session struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	Source       string `json:"source"`
	CWD          string `json:"cwd"`
	SystemPrompt string `json:"system_prompt"`
	ParentID     string `json:"parent_id,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO schema_version (version) VALUES (1)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) SaveSession(sess *Session) error {
	now := time.Now().Unix()
	if sess.CreatedAt == 0 {
		sess.CreatedAt = now
	}
	sess.UpdatedAt = now
	_, err := s.db.Exec(`
		INSERT INTO sessions (id, title, model, provider, source, cwd, system_prompt, parent_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, model=excluded.model, provider=excluded.provider,
			system_prompt=excluded.system_prompt, updated_at=excluded.updated_at
	`, sess.ID, sess.Title, sess.Model, sess.Provider, sess.Source, sess.CWD,
		sess.SystemPrompt, nullStr(sess.ParentID), sess.CreatedAt, sess.UpdatedAt)
	return err
}

func (s *Store) SaveMessages(sessionID string, msgs []Message) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO messages (session_id, role, content, tool_calls, tool_name, usage, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, m := range msgs {
		if m.CreatedAt == 0 {
			m.CreatedAt = now
		}
		m.SessionID = sessionID
		_, err := stmt.Exec(sessionID, m.Role, m.Content,
			nullStr(m.ToolCalls), nullStr(m.ToolName), nullStr(m.Usage), m.CreatedAt)
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Rebuild FTS index so newly inserted messages are searchable.
	s.db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES('rebuild')`)
	return nil
}

func (s *Store) LoadMessages(sessionID string) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, session_id, role, content, COALESCE(tool_calls,''), COALESCE(tool_name,''), COALESCE(usage,''), created_at
		FROM messages WHERE session_id = ? ORDER BY id
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content,
			&m.ToolCalls, &m.ToolName, &m.Usage, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) SearchMessages(query string, limit int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.session_id, m.role, m.content, COALESCE(m.tool_calls,''), COALESCE(m.tool_name,''), COALESCE(m.usage,''), m.created_at
		FROM messages_fts f JOIN messages m ON f.rowid = m.id
		WHERE messages_fts MATCH ? ORDER BY rank LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content,
			&m.ToolCalls, &m.ToolName, &m.Usage, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) LoadSession(id string) (*Session, error) {
	var sess Session
	err := s.db.QueryRow(`
		SELECT id, title, model, provider, source, cwd, system_prompt, COALESCE(parent_id,''), created_at, updated_at
		FROM sessions WHERE id = ?
	`, id).Scan(&sess.ID, &sess.Title, &sess.Model, &sess.Provider,
		&sess.Source, &sess.CWD, &sess.SystemPrompt, &sess.ParentID,
		&sess.CreatedAt, &sess.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) ListSessions(limit int) ([]Session, error) {
	rows, err := s.db.Query(`
		SELECT id, title, model, provider, source, cwd, system_prompt, COALESCE(parent_id,''), created_at, updated_at
		FROM sessions ORDER BY updated_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Model, &sess.Provider,
			&sess.Source, &sess.CWD, &sess.SystemPrompt, &sess.ParentID,
			&sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
