package export

import (
	"encoding/json"
	"testing"

	"go-todo/internal/store"
)

// B3: ToJSON 把 Todo 列表导出为 JSON（含 ID/Title/Done）。
func TestToJSONIncludesFields(t *testing.T) {
	todos := []store.Todo{{ID: 1, Title: "a", Done: false}}
	data, err := ToJSON(todos)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0]["ID"] != float64(1) || out[0]["Title"] != "a" {
		t.Fatalf("unexpected json: %s", data)
	}
}
