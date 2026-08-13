package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"bounty/internal/tool"
)

// EditFileTool applies an exact string replacement with drift tolerance:
// when the exact old_string is missing it retries with whitespace-normalized
// matching, and when both fail the error returns the surrounding lines so the
// model can self-correct and retry.
type EditFileTool struct{}

func (EditFileTool) Name() string      { return "edit_file" }
func (EditFileTool) ReadOnly() bool    { return false }
func (EditFileTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (EditFileTool) Description() string {
	return "Performs exact string replacement in a file. On drift (whitespace/indentation changes) it retries with whitespace-normalized matching; on failure it returns surrounding lines for self-correction."
}
func (EditFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string","maxLength":1024},"old_string":{"type":"string","maxLength":65536},"new_string":{"type":"string","maxLength":1048576},"replace_all":{"type":"boolean"},"context_lines":{"type":"integer","minimum":1,"maximum":200,"description":"On failure, return this many lines of context around the best-guess location (default 20)"}},"required":["file_path","old_string","new_string"],"additionalProperties":false}`)
}

func (EditFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		FilePath     string `json:"file_path"`
		OldString    string `json:"old_string"`
		NewString    string `json:"new_string"`
		ReplaceAll   bool   `json:"replace_all"`
		ContextLines int    `json:"context_lines"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if strings.TrimSpace(params.OldString) == "" {
		return "", fmt.Errorf("old_string must not be empty")
	}
	if params.ContextLines <= 0 {
		params.ContextLines = 20
	}

	data, err := os.ReadFile(params.FilePath)
	if err != nil {
		return "", err
	}
	content := string(data)

	// 1. Exact match (fast path).
	if count := strings.Count(content, params.OldString); count > 0 {
		if count > 1 && !params.ReplaceAll {
			return "", nonUniqueError(params.FilePath, content, params.OldString, count)
		}
		newContent := strings.ReplaceAll(content, params.OldString, params.NewString)
		if err := os.WriteFile(params.FilePath, []byte(newContent), 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Replaced %d occurrence(s) in %s", count, params.FilePath), nil
	}

	// 2. Whitespace-normalized match for drift tolerance.
	oldNorm := normalizeBlock(params.OldString)
	if oldNorm != "" {
		if strings.Contains(oldNorm, "\n") {
			// Multi-line block: whole-line normalized matching.
			starts := normalizedMatchLines(content, oldNorm)
			if len(starts) == 1 {
				newContent, applied, err := applyNormalizedReplace(content, starts[0], params.OldString, params.NewString)
				if err != nil {
					return "", err
				}
				if applied {
					if err := os.WriteFile(params.FilePath, []byte(newContent), 0644); err != nil {
						return "", err
					}
					return fmt.Sprintf("Replaced 1 occurrence in %s (whitespace-normalized match)", params.FilePath), nil
				}
			}
			if len(starts) > 1 {
				return "", fmt.Errorf("old_string 未精确命中，空白归一化后匹配到 %d 处（不唯一）。请提供更具体的 old_string 或使用 replace_all", len(starts))
			}
		} else {
			// Single-line fragment: whitespace-insensitive substring match.
			newContent, n, ok := applyStrippedLineReplace(content, stripped(oldNorm), params.NewString, params.ReplaceAll)
			if ok {
				if n > 1 && !params.ReplaceAll {
					return "", fmt.Errorf("old_string 空白不敏感匹配到 %d 处（不唯一）。请提供更具体的内容或使用 replace_all", n)
				}
				if err := os.WriteFile(params.FilePath, []byte(newContent), 0644); err != nil {
					return "", err
				}
				return fmt.Sprintf("Replaced %d occurrence(s) in %s (whitespace-insensitive match)", n, params.FilePath), nil
			}
		}
	}

	// 2.5 Fully whitespace-stripped match: survives intra-line spacing drift
	// that also spans multiple lines (e.g. "def f( a , b )" vs "def f(a, b)").
	if newContent, n, ok := applyStrippedBlockReplace(content, params.OldString, params.NewString, params.ReplaceAll); ok {
		if n > 1 && !params.ReplaceAll {
			return "", fmt.Errorf("old_string 空白不敏感匹配到 %d 处（不唯一）。请提供更具体的内容或使用 replace_all", n)
		}
		if err := os.WriteFile(params.FilePath, []byte(newContent), 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Replaced %d occurrence(s) in %s (stripped match)", n, params.FilePath), nil
	}

	// 3. Self-healing context: return surrounding lines around the
	// best-guess location.
	return "", driftContextError(params.FilePath, content, params.OldString, params.ContextLines)
}

// nonUniqueError reports the ambiguous occurrences with their line numbers.
func nonUniqueError(path, content, old string, count int) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("old_string 在 %s 中出现 %d 次（不唯一）。请提供更具体的内容。出现位置：\n", path, count))
	lines := strings.Split(content, "\n")
	shown := 0
	search := normalizeBlock(old)
	for i, line := range lines {
		if strings.Contains(line, old) || (search != "" && strings.Contains(normalizeBlock(line), search)) {
			sb.WriteString(fmt.Sprintf("  第 %d 行: %s\n", i+1, truncateLine(line, 120)))
			shown++
			if shown >= 5 {
				break
			}
		}
	}
	return fmt.Errorf("%s", sb.String())
}

// driftContextError builds the "nearby 40 lines" self-healing error.
func driftContextError(path, content, old string, contextLines int) error {
	lines := strings.Split(content, "\n")
	total := len(lines)

	// Best guess: the line whose normalized form shares the longest common
	// prefix with the first normalized line of old_string.
	first := ""
	if fl := strings.Split(normalizeBlock(old), "\n"); len(fl) > 0 {
		first = fl[0]
	}
	best, bestScore := 0, -1
	for i, line := range lines {
		score := lineSimilarityScore(line, first)
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	start := best - contextLines
	if start < 0 {
		start = 0
	}
	end := best + contextLines + 1
	if end > total {
		end = total
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("old_string 未找到（精确匹配与空白归一化匹配均未命中）。文件 %s 共 %d 行，最接近的位置在第 %d 行。\n", path, total, best+1))
	sb.WriteString(fmt.Sprintf("以下是第 %d–%d 行内容，请据此修正 old_string 后重试：\n", start+1, end))
	for i := start; i < end; i++ {
		sb.WriteString(fmt.Sprintf("%6d | %s\n", i+1, truncateLine(lines[i], 200)))
	}
	return fmt.Errorf("%s", sb.String())
}

// lineSimilarityScore rates how close a candidate line is to the first
// normalized line of old_string: word-token containment dominates, and char
// bigram overlap acts as a fallback when spacing drift broke every token.
func lineSimilarityScore(line, target string) int {
	lower := strings.ToLower(line)
	targetLower := strings.ToLower(target)
	score := 0
	for _, tok := range strings.Fields(targetLower) {
		if strings.Contains(lower, tok) {
			score += 100
		}
	}
	want := charBigrams(targetLower)
	if len(want) == 0 {
		return score
	}
	have := make(map[string]bool)
	for _, g := range charBigrams(lower) {
		have[g] = true
	}
	for _, g := range want {
		if have[g] {
			score++
		}
	}
	return score
}

func charBigrams(s string) []string {
	runes := []rune(s)
	if len(runes) < 2 {
		return nil
	}
	out := make([]string, 0, len(runes)-1)
	for i := 0; i+1 < len(runes); i++ {
		out = append(out, string(runes[i:i+2]))
	}
	return out
}

func truncateLine(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func commonPrefixLen(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n := len(ra)
	if len(rb) < n {
		n = len(rb)
	}
	i := 0
	for i < n && ra[i] == rb[i] {
		i++
	}
	return i
}

// normalizeBlock normalizes whitespace so matching survives indentation,
// tab/space, trailing-whitespace, CRLF and blank-line drift.
func normalizeBlock(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	var norm []string
	for _, line := range lines {
		n := strings.Join(strings.Fields(line), " ")
		norm = append(norm, n)
	}
	// Collapse runs of blank lines to a single blank.
	var out []string
	for _, n := range norm {
		if n == "" && len(out) > 0 && out[len(out)-1] == "" {
			continue
		}
		out = append(out, n)
	}
	// Trim leading/trailing blank lines.
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// normalizedMatchLines returns the 0-based start line of every occurrence of
// the normalized block in the file (block boundaries respected).
func normalizedMatchLines(content, oldNorm string) []int {
	rawLines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n"), "\n")
	norm := make([]string, len(rawLines))
	for i, line := range rawLines {
		norm[i] = strings.Join(strings.Fields(line), " ")
	}
	// Collapse blank lines the same way as normalizeBlock while keeping the
	// mapping to original lines.
	type pair struct{ orig int }
	var collapsed []int
	prevBlank := false
	for i, n := range norm {
		if n == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
		} else {
			prevBlank = false
		}
		collapsed = append(collapsed, i)
	}
	joined := make([]string, len(collapsed))
	for j, orig := range collapsed {
		joined[j] = norm[orig]
	}
	text := strings.Join(joined, "\n")

	var starts []int
	from := 0
	for {
		idx := strings.Index(text[from:], oldNorm)
		if idx < 0 {
			break
		}
		abs := from + idx
		// Block boundaries: preceding must be start or newline; following
		// must be end or newline.
		okStart := abs == 0 || text[abs-1] == '\n'
		end := abs + len(oldNorm)
		okEnd := end == len(text) || text[end] == '\n'
		if okStart && okEnd {
			lineIdx := 0
			if abs > 0 {
				lineIdx = strings.Count(text[:abs], "\n")
			}
			if lineIdx < len(collapsed) {
				starts = append(starts, collapsed[lineIdx])
			}
		}
		from = abs + 1
		if from >= len(text) {
			break
		}
	}
	return starts
}

// applyNormalizedReplace swaps the region starting at startLine (matching the
// normalized old_string) with new_string, preserving the file's line endings.
func applyNormalizedReplace(content string, startLine int, oldString, newString string) (string, bool, error) {
	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	oldNormLines := strings.Split(normalizeBlock(oldString), "\n")
	if startLine < 0 || startLine+len(oldNormLines) > len(lines) {
		return content, false, fmt.Errorf("normalized match out of range")
	}
	// Byte offsets in the original content.
	offset := 0
	consumed := 0
	for i := 0; i < startLine; i++ {
		offset += len(lines[i]) + len(eol)
		consumed++
	}
	regionLen := 0
	for i := 0; i < len(oldNormLines); i++ {
		if i > 0 {
			regionLen += len(eol)
		}
		regionLen += len(lines[startLine+i])
	}
	_ = consumed
	newContent := content[:offset] + newString + content[offset+regionLen:]
	return newContent, true, nil
}

// stripped removes every whitespace rune so whitespace-insensitive matching
// survives intra-line spacing drift.
func stripped(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// applyStrippedLineReplace replaces occurrences of the whitespace-stripped
// old fragment inside single lines, mapping matches back to the original
// byte ranges so only the matched span is swapped.
func applyStrippedLineReplace(content, oldStripped, newString string, replaceAll bool) (string, int, bool) {
	if oldStripped == "" {
		return content, 0, false
	}
	norm := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(norm, "\n")

	total := 0
	var out []string
	for _, line := range lines {
		replaced, n, _ := replaceStrippedInLine(line, oldStripped, newString, replaceAll)
		total += n
		out = append(out, replaced)
	}
	if total == 0 {
		return content, 0, false
	}
	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	return strings.Join(out, eol), total, true
}

func replaceStrippedInLine(line, oldStripped, newString string, replaceAll bool) (string, int, bool) {
	st := stripped(line)
	if !strings.Contains(st, oldStripped) {
		return line, 0, false
	}
	var positions []int
	pos := 0
	for {
		idx := strings.Index(st[pos:], oldStripped)
		if idx < 0 {
			break
		}
		positions = append(positions, pos+idx)
		pos += idx + len(oldStripped)
		if !replaceAll {
			break
		}
	}
	var sb strings.Builder
	cursor := 0
	for _, p := range positions {
		start, end := strippedRangeToBytes(line, p, p+len(oldStripped))
		if start < 0 || end < 0 {
			return line, 0, false
		}
		sb.WriteString(line[cursor:start])
		sb.WriteString(newString)
		cursor = end
	}
	sb.WriteString(line[cursor:])
	return sb.String(), len(positions), true
}

// strippedRangeToBytes maps a [start,end) range in whitespace-stripped rune
// space back to byte offsets in the original line.
func strippedRangeToBytes(line string, start, end int) (int, int) {
	byteStart, byteEnd := -1, -1
	nonws := 0
	for i, r := range line {
		if unicode.IsSpace(r) {
			continue
		}
		if nonws == start {
			byteStart = i
		}
		if nonws == end-1 {
			byteEnd = i + utf8.RuneLen(r)
		}
		nonws++
	}
	return byteStart, byteEnd
}

// lfNormWithMap converts CRLF/CR to LF and records, for every byte of the
// normalized string, the byte offset in the original content it came from.
func lfNormWithMap(content string) (string, []int) {
	var b strings.Builder
	b.Grow(len(content))
	mapping := make([]int, 0, len(content))
	i := 0
	for i < len(content) {
		c := content[i]
		if c == '\r' {
			b.WriteByte('\n')
			mapping = append(mapping, i)
			if i+1 < len(content) && content[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
			continue
		}
		b.WriteByte(c)
		mapping = append(mapping, i)
		i++
	}
	return b.String(), mapping
}

// applyStrippedBlockReplace matches the fully whitespace-stripped old_string
// anywhere in the file (single or multi-line) and swaps the matched byte span
// with new_string, preserving the file's line endings. Returns the number of
// matches so the caller can reject ambiguity.
func applyStrippedBlockReplace(content, old, newString string, replaceAll bool) (string, int, bool) {
	oldStripped := stripped(old)
	if oldStripped == "" {
		return content, 0, false
	}
	norm, mapping := lfNormWithMap(content)
	strippedNorm := stripped(norm)

	var positions [][2]int // [start,end) byte offsets in norm
	pos := 0
	for {
		idx := strings.Index(strippedNorm[pos:], oldStripped)
		if idx < 0 {
			break
		}
		start, end := strippedRangeToBytes(norm, pos+idx, pos+idx+len(oldStripped))
		if start < 0 || end < 0 {
			break
		}
		positions = append(positions, [2]int{start, end})
		pos += idx + len(oldStripped)
	}
	if len(positions) == 0 {
		return content, 0, false
	}
	if !replaceAll && len(positions) > 1 {
		return content, len(positions), true
	}
	var sb strings.Builder
	cursor := 0
	for _, p := range positions {
		origStart := mapping[p[0]]
		origEnd := mapping[p[1]-1] + 1
		sb.WriteString(content[cursor:origStart])
		sb.WriteString(newString)
		cursor = origEnd
	}
	sb.WriteString(content[cursor:])
	return sb.String(), len(positions), true
}
