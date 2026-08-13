package plugin

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeZip builds an in-memory zip with the given files and returns it plus
// its sha256 hex digest.
func makeZip(t *testing.T, files map[string]string) ([]byte, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:])
}

// marketServer serves a registry plus one zip per entry.
func marketServer(t *testing.T, entries []MarketplaceEntry, files map[string]string) *httptest.Server {
	t.Helper()
	zips := make(map[string][]byte, len(entries))
	var withHash []MarketplaceEntry
	for _, e := range entries {
		data, sum := makeZip(t, files)
		zips[e.Name] = data
		e.SHA256 = sum
		withHash = append(withHash, e)
	}
	entries = withHash
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/registry.json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, "[")
			for i, e := range entries {
				if i > 0 {
					fmt.Fprintf(w, ",")
				}
				fmt.Fprintf(w, `{"name":%q,"version":%q,"description":%q,"author":%q,"download":%q,"sha256":%q}`,
					e.Name, e.Version, e.Description, e.Author, srv.URL+"/zip/"+e.Name, e.SHA256)
			}
			fmt.Fprintf(w, "]")
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/zip/")
		if data, ok := zips[name]; ok {
			w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	return srv
}

func TestFetchIndexAndSearch(t *testing.T) {
	srv := marketServer(t, []MarketplaceEntry{
		{Name: "security-review", Version: "1.0.0", Description: "安全评审技能", Author: "BJC666"},
		{Name: "docs-zh", Version: "0.2.0", Description: "中文文档助手", Author: "BJC666"},
	}, map[string]string{"plugin.toml": "name = \"security-review\""})
	defer srv.Close()

	entries, err := FetchIndex(context.Background(), srv.URL+"/registry.json")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	// name search
	if got := Search(entries, "安全"); len(got) != 1 || got[0].Name != "security-review" {
		t.Fatalf("search 安全 = %+v", got)
	}
	// description search
	if got := Search(entries, "文档"); len(got) != 1 || got[0].Name != "docs-zh" {
		t.Fatalf("search 文档 = %+v", got)
	}
	// empty query returns all
	if got := Search(entries, ""); len(got) != 2 {
		t.Fatalf("empty search = %d entries", len(got))
	}
}

func TestInstallVerifiesAndExtracts(t *testing.T) {
	files := map[string]string{
		"plugin.toml":        "name = \"security-review\"",
		"commands/review.md": "---\nname: review\ndescription: 安全评审\n---\n评审清单",
	}
	srv := marketServer(t, []MarketplaceEntry{{Name: "security-review", Version: "1.0.0"}}, files)
	defer srv.Close()

	entries, err := FetchIndex(context.Background(), srv.URL+"/registry.json")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	entry, ok := Find(entries, "security-review")
	if !ok {
		t.Fatalf("entry not found")
	}
	destRoot := t.TempDir()
	dest, err := Install(context.Background(), entry, destRoot)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "plugin.toml")); err != nil {
		t.Fatalf("plugin.toml missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "commands", "review.md")); err != nil {
		t.Fatalf("command missing: %v", err)
	}
	if got := Installed(destRoot); len(got) != 1 || got[0] != "security-review" {
		t.Fatalf("installed = %v", got)
	}
}

func TestInstallRejectsBadHash(t *testing.T) {
	files := map[string]string{"plugin.toml": "name = \"evil\""}
	srv := marketServer(t, []MarketplaceEntry{{Name: "evil", Version: "1.0.0"}}, files)
	defer srv.Close()
	entries, err := FetchIndex(context.Background(), srv.URL+"/registry.json")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	entry := entries[0]
	entry.SHA256 = strings.Repeat("0", 64)
	if _, err := Install(context.Background(), entry, t.TempDir()); err == nil {
		t.Fatal("expected sha256 mismatch error")
	}
}

func TestInstallRejectsZipSlip(t *testing.T) {
	// build a zip whose entry tries to escape via ..
	path := filepath.Join(t.TempDir(), "slip.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../evil.md")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("bad"))
	zw.Close()
	f.Close()
	data, _ := os.ReadFile(path)
	sum := sha256.Sum256(data)

	entry := MarketplaceEntry{Name: "slip", Download: "file://" + path, SHA256: hex.EncodeToString(sum[:])}
	if _, err := Install(context.Background(), entry, t.TempDir()); err == nil {
		t.Fatal("expected zip-slip rejection")
	}
}
