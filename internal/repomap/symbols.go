package repomap

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Symbol is a single indexed code symbol.
type Symbol struct {
	Name string
	Kind string
	File string
	Line int
}

// symbolPatterns maps language extension → symbol kind → regex. It is the
// single source of truth shared by the repo map and the code_index tool.
var symbolPatterns = map[string]map[string]*regexp.Regexp{
	".go": {
		"function": regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?(\w+)\s*\(`),
		"type":     regexp.MustCompile(`^type\s+(\w+)\s+(?:struct|interface|int|string|float|bool|map|chan|\[)`),
		"method":   regexp.MustCompile(`^func\s+\([^)]*\*?\w+\)\s+(\w+)\s*\(`),
	},
	".py": {
		"function": regexp.MustCompile(`^def\s+(\w+)\s*\(`),
		"type":     regexp.MustCompile(`^class\s+(\w+)\s*[:(]`),
		"method":   regexp.MustCompile(`^\s+def\s+(\w+)\s*\(self`),
	},
	".ts": {
		"function": regexp.MustCompile(`(?:function|async function)\s+(\w+)\s*\(`),
		"type":     regexp.MustCompile(`(?:interface|type|class)\s+(\w+)`),
		"method":   regexp.MustCompile(`(?:public|private|protected)?\s*(?:async\s+)?(\w+)\s*\([^)]*\)\s*:`),
	},
	".tsx": {
		"function": regexp.MustCompile(`(?:function|const)\s+(\w+)`),
		"type":     regexp.MustCompile(`(?:interface|type|class)\s+(\w+)`),
	},
	".js": {
		"function": regexp.MustCompile(`(?:function|async function)\s+(\w+)\s*\(`),
		"type":     regexp.MustCompile(`(?:class)\s+(\w+)`),
	},
	".rs": {
		"function": regexp.MustCompile(`^(\s*)fn\s+(\w+)\s*[<(]`),
		"type":     regexp.MustCompile(`^(?:\s*)pub?\s*(?:struct|enum|trait|type)\s+(\w+)`),
	},
}

// SupportedExt reports whether symbols can be indexed for the file extension.
func SupportedExt(path string) bool {
	_, ok := symbolPatterns[strings.ToLower(filepath.Ext(path))]
	return ok
}

// IndexFile scans one file for symbols of the given kind ("all" = every
// kind). Results are capped at max per file.
func IndexFile(path string, kindFilter string, max int) []Symbol {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	ext := strings.ToLower(filepath.Ext(path))
	patterns, ok := symbolPatterns[ext]
	if !ok {
		return nil
	}

	var results []Symbol
	for i, line := range lines {
		for kind, re := range patterns {
			if kindFilter != "all" && kindFilter != kind {
				continue
			}
			matches := re.FindStringSubmatch(line)
			if matches == nil {
				continue
			}
			name := matches[len(matches)-1] // last capture group
			if name == "" {
				continue
			}
			results = append(results, Symbol{Name: name, Kind: kind, File: path, Line: i + 1})
			if max > 0 && len(results) >= max {
				return results
			}
		}
	}
	return results
}
