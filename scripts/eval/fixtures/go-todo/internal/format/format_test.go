package format

import (
	"strings"
	"testing"

	"go-todo/internal/store"
)

func TestFormatListMarksDone(t *testing.T) {
	todos := []store.Todo{{ID: 1, Title: "a", Done: false}, {ID: 2, Title: "b", Done: true}}
	out := FormatList(todos)
	if !strings.Contains(out, "[x] b") {
		t.Fatalf("expected done marker: %q", out)
	}
}
