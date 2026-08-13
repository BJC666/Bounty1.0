package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LoadEntries reads every memory entry under <root>/.agent/memory and returns
// them in directory order (call Recent for recency ordering). MEMORY.md (the
// index file) is skipped.
func LoadEntries(projectRoot string) ([]Entry, error) {
	rs := NewRememberStore(projectRoot)
	dirEntries, err := os.ReadDir(rs.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, de := range dirEntries {
		if de.IsDir() || de.Name() == "MEMORY.md" || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rs.dir, de.Name()))
		if err != nil {
			continue
		}
		out = append(out, parseEntry(data, de.Name()))
	}
	return out, nil
}

// parseEntry extracts frontmatter (name/description/created) plus body
// content from a memory file. Missing fields fall back to the file name and
// the raw text.
func parseEntry(data []byte, filename string) Entry {
	e := Entry{Name: strings.TrimSuffix(filename, ".md")}
	text := string(data)
	body := strings.TrimSpace(text)

	if strings.HasPrefix(text, "---") {
		if end := strings.Index(text[3:], "---"); end >= 0 {
			fm := text[3 : 3+end]
			body = strings.TrimSpace(text[3+end+3:])
			for _, line := range strings.Split(fm, "\n") {
				line = strings.TrimSpace(line)
				switch {
				case strings.HasPrefix(line, "name:"):
					if v := strings.TrimSpace(strings.TrimPrefix(line, "name:")); v != "" {
						e.Name = v
					}
				case strings.HasPrefix(line, "description:"):
					e.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
				case strings.HasPrefix(line, "created:"):
					if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(strings.TrimPrefix(line, "created:"))); err == nil {
						e.CreatedAt = ts
					}
				}
			}
		}
	}
	e.Content = body
	return e
}

// Recent returns up to limit entries ordered newest-first. Entries without a
// parsed timestamp sort before timed ones.
func Recent(projectRoot string, limit int) ([]Entry, error) {
	entries, err := LoadEntries(projectRoot)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	return entries[:limit], nil
}

// Search scores memory entries against a free-text query and returns up to
// limit results, best match first. It is a small keyword/bigram scorer over
// the file-backed auto-memory store (no external index dependency): the full
// query, latin tokens and CJK bigrams each contribute, weighted by field
// importance (name > description > content).
func Search(projectRoot, query string, limit int) ([]Entry, error) {
	entries, err := LoadEntries(projectRoot)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}

	needles := searchNeedles(q)
	type scored struct {
		e     Entry
		score int
	}
	var out []scored
	for _, e := range entries {
		name := strings.ToLower(e.Name)
		desc := strings.ToLower(e.Description)
		content := strings.ToLower(e.Content)
		score := 0
		for _, n := range needles {
			if n == "" {
				continue
			}
			if strings.Contains(name, n) {
				score += 50 * needleWeight(n)
			}
			if strings.Contains(desc, n) {
				score += 30 * needleWeight(n)
			}
			if strings.Contains(content, n) {
				score += 10 * needleWeight(n)
			}
		}
		if score > 0 {
			out = append(out, scored{e: e, score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].e.CreatedAt.After(out[j].e.CreatedAt)
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	res := make([]Entry, limit)
	for i := 0; i < limit; i++ {
		res[i] = out[i].e
	}
	return res, nil
}

// searchNeedles builds the match tokens for a query: the full query itself,
// ascii/latin words of length >= 2, and CJK bigrams.
func searchNeedles(q string) []string {
	var out []string
	if len(q) >= 1 {
		out = append(out, q)
	}
	// latin words
	start := -1
	for i, r := range q {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 && i-start >= 1 {
			out = append(out, q[start:i])
		}
		start = -1
	}
	if start >= 0 && len(q)-start >= 1 {
		out = append(out, q[start:])
	}
	// CJK bigrams (rune-based)
	runes := []rune(q)
	for i := 0; i+1 < len(runes); i++ {
		a, b := runes[i], runes[i+1]
		if isCJK(a) && isCJK(b) {
			out = append(out, string(a)+string(b))
		}
	}
	return out
}

func isCJK(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

// needleWeight gives longer needles more weight so exact phrase hits beat
// single-character noise.
func needleWeight(n string) int {
	w := len([]rune(n))
	if w > 4 {
		return 4
	}
	if w < 2 {
		return 1
	}
	return w
}
