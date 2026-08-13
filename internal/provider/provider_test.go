package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalPNG is a 1x1 transparent PNG (67 bytes); DetectContentType sniffs
// the magic header and reports image/png.
var minimalPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func TestNewUserMessageTextOnly(t *testing.T) {
	m := NewUserMessage("hello")
	if m.Content != "hello" || len(m.Parts) != 0 {
		t.Fatalf("text-only message must keep legacy shape: %+v", m)
	}
}

func TestNewUserMessageWithImages(t *testing.T) {
	img := ImagePart{Data: "QUJD", MediaType: "image/png"}
	m := NewUserMessage("看图", img)
	if m.Content != "" || len(m.Parts) != 2 {
		t.Fatalf("multimodal shape: %+v", m)
	}
	if m.Parts[0].Type != "text" || m.Parts[0].Text != "看图" {
		t.Fatalf("text part: %+v", m.Parts[0])
	}
	if m.Parts[1].Type != "image" || m.Parts[1].Image == nil || m.Parts[1].Image.Data != "QUJD" {
		t.Fatalf("image part: %+v", m.Parts[1])
	}
}

func TestLoadImageFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "err.png")
	if err := os.WriteFile(p, minimalPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := LoadImageFile(p)
	if err != nil {
		t.Fatalf("LoadImageFile: %v", err)
	}
	if img.MediaType != "image/png" {
		t.Fatalf("media type = %q", img.MediaType)
	}
	if !strings.HasPrefix(img.Data, "iVBOR") {
		t.Fatalf("expected base64 PNG prefix, got %q", img.Data[:8])
	}
}

func TestLoadImageFileRejectsTextFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(p, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImageFile(p); err == nil {
		t.Fatal("expected rejection for non-image file")
	}
}
