package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var pngHeader = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}

func TestExtractImagePaths(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "err shot.png")
	if err := os.WriteFile(img, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}
	img2 := filepath.Join(dir, "second.png")
	if err := os.WriteFile(img2, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}

	clean, paths := extractImagePaths(`看看这个报错 "` + img + `" 再对比 ` + img2 + ` 和不存在.png`)
	if len(paths) != 2 || paths[0] != img || paths[1] != img2 {
		t.Fatalf("paths = %v", paths)
	}
	if strings.Contains(clean, "err shot") || strings.Contains(clean, "second.png") {
		t.Fatalf("existing image paths must be stripped from text: %q", clean)
	}
	if !strings.Contains(clean, "看看这个报错") || !strings.Contains(clean, "不存在.png") {
		t.Fatalf("text mangled: %q", clean)
	}
}

func TestExtractImagePathsCapAtFour(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for i := 0; i < 6; i++ {
		p := filepath.Join(dir, string(rune('a'+i))+".png")
		if err := os.WriteFile(p, pngHeader, 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}
	clean, paths := extractImagePaths(strings.Join(files, " "))
	if len(paths) != maxTUIImages {
		t.Fatalf("cap = %d, got %d paths", maxTUIImages, len(paths))
	}
	if !strings.Contains(clean, "e.png") {
		t.Fatalf("extra paths must remain as text: %q", clean)
	}
}
