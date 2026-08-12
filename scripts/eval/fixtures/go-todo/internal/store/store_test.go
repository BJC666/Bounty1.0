package store

import "testing"

func TestAddAndList(t *testing.T) {
	s := New()
	id := s.Add("buy milk")
	if id != 1 {
		t.Fatalf("want id 1, got %d", id)
	}
	items := s.List()
	if len(items) != 1 || items[0].Title != "buy milk" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestListIsCopy(t *testing.T) {
	s := New()
	s.Add("a")
	items := s.List()
	items[0].Title = "mutated"
	if s.items[0].Title != "a" {
		t.Fatal("List must return a copy")
	}
}
