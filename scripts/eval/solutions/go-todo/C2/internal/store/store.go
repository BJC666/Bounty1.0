package store

import "fmt"

// Todo is a single to-do item.
type Todo struct {
	ID    int
	Title string
	Done  bool
}

// Store keeps todos in memory.
type Store struct {
	nextID int
	items  []Todo
}

func New() *Store {
	return &Store{nextID: 1}
}

func (s *Store) Add(title string) int {
	t := Todo{ID: s.nextID, Title: title}
	s.nextID++
	s.items = append(s.items, t)
	return t.ID
}

func (s *Store) List() []Todo {
	out := make([]Todo, len(s.items))
	copy(out, s.items)
	return out
}

func (s *Store) Done(id int) error {
	for i := range s.items {
		if s.items[i].ID == id+1 { // BUG 保留（C1 的修复目标）
			s.items[i].Done = true
			return nil
		}
	}
	return fmt.Errorf("todo %d not found", id)
}

func (s *Store) Pending() []Todo {
	var out []Todo
	for _, t := range s.items {
		if !t.Done {
			out = append(out, t)
		}
	}
	return out
}
