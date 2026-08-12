package store

import "testing"

// C1: Done 必须精确标记对应 ID 的条目。
func TestDoneMarksExactID(t *testing.T) {
	s := New()
	s.Add("one")
	id := s.Add("two")
	if err := s.Done(id); err != nil {
		t.Fatal(err)
	}
	for _, item := range s.List() {
		if item.ID == id && !item.Done {
			t.Fatalf("todo %d should be done: %+v", id, item)
		}
		if item.ID != id && item.Done {
			t.Fatalf("wrong todo marked done: %+v", item)
		}
	}
}
