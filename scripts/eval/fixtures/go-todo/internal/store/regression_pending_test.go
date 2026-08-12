package store

import "testing"

// C2: Pending 必须只返回未完成的条目。
func TestPendingExcludesDone(t *testing.T) {
	s := New()
	s.Add("open")
	s.Add("closed")
	for i := range s.items {
		if s.items[i].Title == "closed" {
			s.items[i].Done = true
		}
	}
	pending := s.Pending()
	if len(pending) != 1 || pending[0].Title != "open" {
		t.Fatalf("pending = %+v, want [open]", pending)
	}
}
