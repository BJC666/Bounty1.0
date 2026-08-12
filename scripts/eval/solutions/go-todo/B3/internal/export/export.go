package export

import (
	"encoding/json"

	"go-todo/internal/store"
)

// ToJSON serializes todos with 2-space indentation.
func ToJSON(todos []store.Todo) ([]byte, error) {
	return json.MarshalIndent(todos, "", "  ")
}
