package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"bounty/internal/tool"
)

type WriteFileTool struct{}

func (WriteFileTool) Name() string        { return "write_file" }
func (WriteFileTool) ReadOnly() bool      { return false }
func (WriteFileTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "builtin"} }
func (WriteFileTool) Description() string { return "Creates or overwrites a file at the specified path." }
func (WriteFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string","maxLength":1024,"description":"Absolute file path"},"content":{"type":"string","maxLength":1048576,"description":"Content to write"},"overwrite":{"type":"boolean","description":"Set true to overwrite existing file"}},"required":["file_path","content"],"additionalProperties":false}`)
}
func (WriteFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		FilePath  string `json:"file_path"`
		Content   string `json:"content"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if !params.Overwrite {
		if _, err := os.Stat(params.FilePath); err == nil {
			return "", fmt.Errorf("文件已存在：%s。覆盖已有文件需要显式设置 overwrite:true，防止误覆盖。", params.FilePath)
		}
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
