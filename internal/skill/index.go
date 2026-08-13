package skill

import (
	"fmt"
	"strings"
)

const maxIndexChars = 2500 // P6 裁剪：索引只列名+描述，命中才注入正文

// IndexBlock returns a cache-friendly skill index for the system prompt.
// Only name + description + subagent tag, capped at maxIndexChars.
func (s *Store) IndexBlock() string {
	entries := s.Index()
	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Available Skills\n")
	for _, e := range entries {
		tag := ""
		if e.IsSubagent {
			tag = " [subagent]"
		}
		line := fmt.Sprintf("- %s: %s%s\n", e.Name, e.Description, tag)
		if sb.Len()+len(line) > maxIndexChars {
			break
		}
		sb.WriteString(line)
	}
	return sb.String()
}
