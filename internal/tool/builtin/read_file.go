package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"bounty/internal/tool"
)

// pathHints searches for same-basename files near the requested path and
// returns a short "did you mean" appendix, so a wrong-path read degrades into
// a useful hint instead of a dead end (the dominant tool failure in Eval).
func pathHints(requested string) string {
	base := strings.ToLower(filepath.Base(requested))
	if base == "" || base == "." || base == "/" {
		return ""
	}
	// Walk from the nearest existing ancestor (usually the workspace root).
	dir := filepath.Dir(requested)
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	var candidates []string
	visited := 0
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visited++
		if visited > 4000 {
			return filepath.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "node_modules" || name == "__pycache__" || name == ".venv" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(name, base) && len(candidates) < 5 {
			candidates = append(candidates, path)
		}
		return nil
	})
	if len(candidates) == 0 {
		return ""
	}
	return fmt.Sprintf(" 疑似目标文件：%s。可先 glob 确认后再读。", strings.Join(candidates, "；"))
}

// defaultReadMaxLines caps read_file output unless the caller passes an
// explicit limit, so a huge file cannot flood the context.
const defaultReadMaxLines = 2000

type ReadFileTool struct{}

func (ReadFileTool) Name() string      { return "read_file" }
func (ReadFileTool) ReadOnly() bool    { return true }
func (ReadFileTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (ReadFileTool) Description() string {
	return "Read a file; returns content with line numbers."
}
func (ReadFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string","maxLength":1024,"description":"Absolute file path"},"offset":{"type":"integer","minimum":1,"description":"Start line"},"limit":{"type":"integer","minimum":1,"maximum":10000,"description":"Max lines"}},"required":["file_path"],"additionalProperties":false}`)
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
		return "", fmt.Errorf("%w%s", err, pathHints(params.FilePath))
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
