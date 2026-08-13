package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// imageExts are the extensions treated as paste-able images in the TUI.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

// quotedToken matches double-quoted tokens or bare whitespace-separated
// tokens, so both "D:\a b\err.png" and D:\a\err.png are recognized.
var quotedToken = regexp.MustCompile(`"[^"]+"|\S+`)

// maxTUIimages caps images per message so a paste of a directory listing
// cannot bloat one turn.
const maxTUIImages = 4

// extractImagePaths finds up to maxTUIImages existing image files referenced
// in text, removes those tokens from the returned text, and returns the paths
// in order. Tokens that are not existing image files are left untouched.
func extractImagePaths(text string) (string, []string) {
	var paths []string
	repl := func(tok string) string {
		if len(paths) >= maxTUIImages {
			return tok
		}
		clean := strings.Trim(tok, `"'`)
		if clean == "" {
			return tok
		}
		if !imageExts[strings.ToLower(filepath.Ext(clean))] {
			return tok
		}
		st, err := os.Stat(clean)
		if err != nil || st.IsDir() {
			return tok
		}
		paths = append(paths, clean)
		return ""
	}
	out := quotedToken.ReplaceAllStringFunc(text, repl)
	out = strings.Join(strings.Fields(out), " ")
	return out, paths
}
