package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"bounty/internal/tool"
)

type EditFileTool struct{}

func (EditFileTool) Name() string        { return "edit_file" }
func (EditFileTool) ReadOnly() bool      { return false }
func (EditFileTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "builtin"} }
func (EditFileTool) Description() string { return "Performs exact string replacement in a file. old_string must match exactly and be unique." }
func (EditFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["file_path","old_string","new_string"]}`)
}
func (EditFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	data, err := os.ReadFile(params.FilePath)
	if err != nil {
		return "", err
	}
	content := string(data)
	count := strings.Count(content, params.OldString)
	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", params.FilePath)
	}
	if count > 1 && !params.ReplaceAll {
		return "", fmt.Errorf("old_string found %d times in %s; use replace_all:true or make it more specific", count, params.FilePath)
	}
	newContent := strings.ReplaceAll(content, params.OldString, params.NewString)
	if err := os.WriteFile(params.FilePath, []byte(newContent), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("Replaced %d occurrence(s) in %s", count, params.FilePath), nil
}
