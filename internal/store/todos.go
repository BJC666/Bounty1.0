package store

import (
	"time"
)

// Todo is a single todo_write list item.
type Todo struct {
	ID         int64  `json:"id"`
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form,omitempty"`
}

// ReplaceTodos atomically replaces the todo list of a session with the given
// items (order preserved).
func (s *Store) ReplaceTodos(sessionID string, todos []Todo) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM todos WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	now := time.Now().Unix()
	stmt, err := tx.Prepare(`INSERT INTO todos (session_id, content, status, active_form, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range todos {
		if _, err := stmt.Exec(sessionID, t.Content, t.Status, t.ActiveForm, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadTodos returns the session's todo list in insertion order.
func (s *Store) LoadTodos(sessionID string) ([]Todo, error) {
	rows, err := s.db.Query(`SELECT id, content, status, active_form FROM todos WHERE session_id = ? ORDER BY id ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Todo
	for rows.Next() {
		var t Todo
		if err := rows.Scan(&t.ID, &t.Content, &t.Status, &t.ActiveForm); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
