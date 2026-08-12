package format

import (
	"strings"
	"testing"

	"go-todo/internal/store"
)

// C4: 列表编号必须从 1 开始。
func TestFormatListStartsAtOne(t *testing.T) {
	todos := []store.Todo{{ID: 1, Title: "first", Done: false}}
	out := FormatList(todos)
	if !strings.Contains(out, "1. [ ] first") {
		t.Fatalf("list must start at 1: %q", out)
	}
}
