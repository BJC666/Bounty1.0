package search

import (
	"strings"

	"go-todo/internal/store"
)

// Search returns todos whose title contains keyword (case-insensitive).
func Search(todos []store.Todo, keyword string) []store.Todo {
	var out []store.Todo
	for _, t := range todos {
		if strings.Contains(strings.ToLower(t.Title), strings.ToLower(keyword)) {
			out = append(out, t)
		}
	}
	return out
}
