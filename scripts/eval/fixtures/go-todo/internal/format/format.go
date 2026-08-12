package format

import (
	"fmt"
	"strings"

	"go-todo/internal/store"
)

// FormatList renders todos as a numbered list; "[x]" marks done items.
func FormatList(todos []store.Todo) string {
	var b strings.Builder
	for i, t := range todos {
		status := " "
		if t.Done {
			status = "x"
		}
		fmt.Fprintf(&b, "%d. [%s] %s\n", i, status, t.Title) // BUG: 应为 i+1
	}
	return b.String()
}
