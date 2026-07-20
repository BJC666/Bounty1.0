package store

import (
	"testing"
)

func TestSaveAndLoadSession(t *testing.T) {
	path := t.TempDir() + "/test.db"
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	sess := &Session{ID: "s1", Title: "Test", Model: "deepseek-v4-pro", Provider: "deepseek"}
	if err := s.SaveSession(sess); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadSession("s1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "Test" {
		t.Errorf("expected Title=Test, got %s", loaded.Title)
	}
}

func TestSaveAndLoadMessages(t *testing.T) {
	path := t.TempDir() + "/test.db"
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.SaveSession(&Session{ID: "s1"})
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	if err := s.SaveMessages("s1", msgs); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadMessages("s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded))
	}
}

func TestFTS5Search(t *testing.T) {
	path := t.TempDir() + "/test.db"
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.SaveSession(&Session{ID: "s1"})
	s.SaveMessages("s1", []Message{
		{Role: "user", Content: "how to optimize SQL queries"},
	})

	results, err := s.SearchMessages("optimize", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected FTS5 search results")
	}
}

func TestListSessions(t *testing.T) {
	path := t.TempDir() + "/test.db"
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.SaveSession(&Session{ID: "a", Title: "First"})
	s.SaveSession(&Session{ID: "b", Title: "Second"})

	list, err := s.ListSessions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}
}
