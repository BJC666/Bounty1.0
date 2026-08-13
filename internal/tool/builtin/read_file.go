package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"bounty/internal/tool"
)

// defaultReadMaxLines caps read_file output unless the caller passes an
// explicit limit, so a huge file cannot flood the context.
const defaultReadMaxLines = 2000

type ReadFileTool struct{}

func (ReadFileTool) Name() string      { return "read_file" }
func (ReadFileTool) ReadOnly() bool    { return true }
func (ReadFileTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (ReadFileTool) Description() string {
	return "Reads a file from the local filesystem. Returns content with line numbers."
}
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
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)
	if params.Offset < 0 {
		params.Offset = 0
	}
	if params.Offset >= total {
		return fmt.Sprintf("offset=%d 超出文件范围（文件共 %d 行）。", params.Offset, total), nil
	}
	if params.Offset > 0 {
		lines = lines[params.Offset:]
	}
	limited := false
	if params.Limit > 0 {
		if params.Limit < len(lines) {
			lines = lines[:params.Limit]
			limited = true
		}
	} else if len(lines) > defaultReadMaxLines {
		lines = lines[:defaultReadMaxLines]
		limited = true
	}

	out := strings.Join(lines, "\n")
	if limited {
		shown := len(lines)
		nextOffset := params.Offset + shown
		out += fmt.Sprintf("\n...[read_file truncated: 文件共 %d 行，当前显示第 %d–%d 行。用 offset=%d limit=%d 继续读取。]",
			total, params.Offset+1, params.Offset+shown, nextOffset, defaultReadMaxLines)
	}
	return out, nil
}
