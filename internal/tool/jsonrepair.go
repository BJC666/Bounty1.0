package tool

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RepairToolArgs validates streaming-accumulated tool-call arguments and, when
// the JSON is broken (truncation, trailing commas, unclosed quotes/brackets,
// code fences, trailing prose, bare keys, NaN/Infinity), attempts deterministic
// repairs. It returns the repaired bytes or an error describing the failure.
//
// Repairs are conservative: a result is only accepted when json.Valid confirms
// it, so a failed repair leaves the original bytes untouched.
func RepairToolArgs(raw []byte) ([]byte, error) {
	s := strings.TrimSpace(strings.TrimPrefix(string(raw), "\uFEFF"))
	if s == "" {
		return nil, fmt.Errorf("arguments 为空")
	}
	start := strings.IndexAny(s, `{["`)
	if start < 0 {
		return nil, fmt.Errorf("arguments 中找不到 JSON 起始符 { 或 [")
	}
	s = s[start:]

	// 1. Already valid with possible trailing prose (explanation, code fence
	// tail): cut at the end of the first complete JSON value.
	if cut, ok := validPrefixCut(s); ok {
		return []byte(cut), nil
	}

	// 2. Structural repair pass (quotes, trailing commas, missing closers).
	if fixed, ok := repairStructure(s); ok {
		return []byte(fixed), nil
	}

	// 3. Same, but on the longest valid prefix (drops trailing garbage that
	// defeated the structural pass).
	if cut, ok := validPrefixCut(s); ok {
		if fixed, ok := repairStructure(cut); ok {
			return []byte(fixed), nil
		}
	}

	return nil, fmt.Errorf("JSON 修复失败（已尝试补尾括号、去尾逗号、修引号）：%s", truncateForError(s))
}

// validPrefixCut returns the longest prefix of s that is one complete JSON
// value, using the decoder's input offset.
func validPrefixCut(s string) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	return s[:dec.InputOffset()], true
}

// repairStructure rebuilds s into valid JSON: single/curly quotes become
// double quotes, trailing commas before closers are dropped, and any unclosed
// strings/brackets are closed at the end.
func repairStructure(s string) (string, bool) {
	var out strings.Builder
	stack := make([]byte, 0, 8)
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		switch {
		case c == '"' || c == '\'' || strings.HasPrefix(s[i:], "\u201C") || strings.HasPrefix(s[i:], "\u2018"):
			closerStr := `"`
			consume := 1
			switch {
			case c == '\'':
				closerStr = `'`
			case strings.HasPrefix(s[i:], "\u201C"):
				closerStr = "\u201D"
				consume = 3
			case strings.HasPrefix(s[i:], "\u2018"):
				closerStr = "\u2019"
				consume = 3
			}
			j := i + consume
			var body strings.Builder
			for j < n {
				if strings.HasPrefix(s[j:], closerStr) {
					j += len(closerStr)
					break
				}
				ch := s[j]
				if ch == '\\' && j+1 < n {
					nxt := s[j+1]
					switch {
					case nxt == '\'':
						body.WriteString(`\"`)
					case strings.ContainsRune(`"\/bfnrtu`, rune(nxt)):
						body.WriteByte('\\')
						body.WriteByte(nxt)
					default:
						// Invalid JSON escape (e.g. Windows path "\U"): keep
						// the backslash as a literal.
						body.WriteString(`\\`)
						body.WriteByte(nxt)
					}
					j += 2
					continue
				}
				if ch == '"' {
					body.WriteString(`\"`)
				} else if ch == '\n' {
					body.WriteString(`\n`)
				} else {
					body.WriteByte(ch)
				}
				j++
			}
			out.WriteByte('"')
			out.WriteString(body.String())
			out.WriteByte('"')
			i = j
		case c == '{' || c == '[':
			out.WriteByte(c)
			if c == '{' {
				stack = append(stack, '}')
			} else {
				stack = append(stack, ']')
			}
			i++
		case c == '}' || c == ']':
			trimTrailingComma(&out)
			if len(stack) > 0 && stack[len(stack)-1] == c {
				stack = stack[:len(stack)-1]
			}
			out.WriteByte(c)
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	for len(stack) > 0 {
		trimTrailingComma(&out)
		closer := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out.WriteByte(closer)
	}
	res := fixNaN(out.String())
	if !json.Valid([]byte(res)) {
		// Bare keys such as {file_path: "x"} are invalid; quote them.
		if q, ok := quoteBareKeys(res); ok {
			return q, true
		}
		return "", false
	}
	return res, true
}

// trimTrailingComma removes the trailing comma just before a closing bracket.
func trimTrailingComma(out *strings.Builder) {
	s := out.String()
	if idx := len(s) - 1; idx >= 0 && s[idx] == ',' {
		// Rewind the builder (only valid because we append whole bytes).
		out.Reset()
		out.WriteString(s[:idx])
	}
}

// quoteBareKeys wraps unquoted object keys in double quotes.
func quoteBareKeys(s string) (string, bool) {
	var out strings.Builder
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		if c == '"' {
			j := i + 1
			for j < n {
				if s[j] == '\\' {
					j += 2
					continue
				}
				if s[j] == '"' {
					j++
					break
				}
				j++
			}
			out.WriteString(s[i:j])
			i = j
			continue
		}
		if c == '{' || c == ',' {
			out.WriteByte(c)
			i++
			k := i
			for k < n && (s[k] == ' ' || s[k] == '\t' || s[k] == '\r' || s[k] == '\n') {
				k++
			}
			if k < n && isBareKeyStart(s[k]) {
				e := k
				for e < n && isBareKeyChar(s[e]) {
					e++
				}
				m := e
				for m < n && (s[m] == ' ' || s[m] == '\t') {
					m++
				}
				if m < n && s[m] == ':' {
					out.WriteByte('"')
					out.WriteString(s[k:e])
					out.WriteByte('"')
					i = m
					continue
				}
			}
			continue
		}
		out.WriteByte(c)
		i++
	}
	res := out.String()
	if !json.Valid([]byte(res)) {
		return "", false
	}
	return res, true
}

func isBareKeyStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isBareKeyChar(c byte) bool {
	return isBareKeyStart(c) || (c >= '0' && c <= '9') || c == '.' || c == '-'
}

// fixNaN rewrites NaN / Infinity / -Infinity outside strings to null.
func fixNaN(s string) string {
	var out strings.Builder
	inStr := false
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		if c == '"' {
			inStr = !inStr
			out.WriteByte(c)
			i++
			continue
		}
		if inStr {
			out.WriteByte(c)
			i++
			continue
		}
		switch {
		case strings.HasPrefix(s[i:], "NaN"):
			out.WriteString("null")
			i += 3
		case strings.HasPrefix(s[i:], "Infinity"):
			if out.Len() > 0 && out.String()[out.Len()-1] == '-' {
				cur := out.String()
				out.Reset()
				out.WriteString(cur[:len(cur)-1])
			}
			out.WriteString("null")
			i += 8
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

func truncateForError(s string) string {
	r := []rune(s)
	if len(r) <= 200 {
		return s
	}
	return string(r[:200]) + "...(截断)"
}
