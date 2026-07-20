package builtin

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"unicode/utf8"
)

type ReadFileTool struct{}

func (ReadFileTool) Name() string        { return "read_file" }
func (ReadFileTool) ReadOnly() bool      { return true }
func (ReadFileTool) Description() string { return "Reads a file from the local filesystem. Returns content with line numbers." }
func (ReadFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string","description":"The absolute path to the file to read"},"offset":{"type":"integer","description":"Line number to start reading from"},"limit":{"type":"integer","description":"Number of lines to read"}},"required":["file_path"]}`)
}
func (ReadFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		FilePath string `json:"file_path"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	data, err := os.ReadFile(params.FilePath)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "[binary file]", nil
	}
	lines := strings.Split(string(data), "\n")
	if params.Offset > 0 && params.Offset < len(lines) {
		lines = lines[params.Offset:]
	}
	if params.Limit > 0 && params.Limit < len(lines) {
		lines = lines[:params.Limit]
	}
	return strings.Join(lines, "\n"), nil
}
