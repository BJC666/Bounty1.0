package search

import (
	"testing"

	"go-todo/internal/store"
)

// B1: Search 按关键词在标题中模糊搜索（大小写不敏感）。
func TestSearchFindsSubstring(t *testing.T) {
	s := store.New()
	s.Add("buy milk")
	s.Add("write report")
	results := Search(s.List(), "milk")
	if len(results) != 1 || results[0].Title != "buy milk" {
		t.Fatalf("results = %+v, want [buy milk]", results)
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	s := store.New()
	s.Add("Buy Milk")
	results := Search(s.List(), "milk")
	if len(results) != 1 {
		t.Fatalf("search must be case-insensitive, got %+v", results)
	}
}
