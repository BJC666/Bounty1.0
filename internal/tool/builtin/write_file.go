package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

type WriteFileTool struct{}

func (WriteFileTool) Name() string        { return "write_file" }
func (WriteFileTool) ReadOnly() bool      { return false }
func (WriteFileTool) Description() string { return "Creates or overwrites a file at the specified path." }
func (WriteFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string","description":"The absolute path to the file to write"},"content":{"type":"string","description":"The content to write to the file"}},"required":["file_path","content"]}`)
}
func (WriteFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	dir := filepath.Dir(params.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(params.FilePath, []byte(params.Content), 0644); err != nil {
		return "", err
	}
	return "File written successfully: " + params.FilePath, nil
}
