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
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, status, t.Title)
	}
	return b.String()
}
