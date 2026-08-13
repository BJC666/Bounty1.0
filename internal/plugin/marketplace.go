package plugin

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MarketplaceEntry is one row of the plugin registry (a JSON array fetched
// from the marketplace URL). Download points at a zip archive of the plugin
// directory (plugin.toml + commands/ + agents/ ...); SHA256 pins the archive.
type MarketplaceEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Download    string `json:"download"`
	SHA256      string `json:"sha256"`
}

// DefaultRegistryURL is the marketplace index shipped with Bounty. It is
// overridable via BOUNTY_PLUGIN_REGISTRY or --registry.
const DefaultRegistryURL = "https://raw.githubusercontent.com/BJC666/Bounty1.0/main/marketplace/registry.json"

var marketplaceHTTPClient = &http.Client{Timeout: 30 * time.Second}

// githubMirrors are trusted GitHub proxies (AGENTS.md whitelist) tried in
// order when the direct connection fails — common for mainland-China
// networks. Only github.com URLs are retried via mirrors.
var githubMirrors = []string{
	"https://gh-proxy.com/",
	"https://ghfast.top/",
}

// getWithMirror GETs url directly; on network failure it retries through the
// trusted GitHub mirrors (direct-only for non-github hosts).
func getWithMirror(ctx context.Context, url string) (*http.Response, error) {
	do := func(target string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		return marketplaceHTTPClient.Do(req)
	}
	resp, err := do(url)
	if err == nil && resp.StatusCode < 400 {
		return resp, nil
	}
	if err == nil {
		resp.Body.Close()
		err = fmt.Errorf("status %d", resp.StatusCode)
	}
	if !strings.HasPrefix(url, "https://github.com/") && !strings.HasPrefix(url, "https://raw.githubusercontent.com/") {
		return nil, err
	}
	for _, m := range githubMirrors {
		mr, merr := do(m + url)
		if merr == nil && mr.StatusCode < 400 {
			return mr, nil
		}
		if merr == nil {
			mr.Body.Close()
			merr = fmt.Errorf("status %d", mr.StatusCode)
		}
		err = merr
	}
	return nil, err
}

// FetchIndex downloads and parses the marketplace registry.
func FetchIndex(ctx context.Context, url string) ([]MarketplaceEntry, error) {
	if url == "" {
		url = DefaultRegistryURL
	}
	resp, err := getWithMirror(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch marketplace: %w", err)
	}
	defer resp.Body.Close()
	var entries []MarketplaceEntry
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&entries); err != nil {
		return nil, fmt.Errorf("parse marketplace: %w", err)
	}
	return entries, nil
}

// Search filters entries by a case-insensitive substring on name, description
// and author. Empty query returns all entries.
func Search(entries []MarketplaceEntry, query string) []MarketplaceEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return entries
	}
	var out []MarketplaceEntry
	for _, e := range entries {
		hay := strings.ToLower(e.Name + " " + e.Description + " " + e.Author)
		if strings.Contains(hay, q) {
			out = append(out, e)
		}
	}
	return out
}

// Find returns the entry whose name matches exactly (case-insensitive).
func Find(entries []MarketplaceEntry, name string) (MarketplaceEntry, bool) {
	for _, e := range entries {
		if strings.EqualFold(e.Name, name) {
			return e, true
		}
	}
	return MarketplaceEntry{}, false
}

// Install downloads a plugin zip, verifies its SHA256 and extracts it into
// destRoot/<name>. Zip-slip entries (absolute paths or ..) are refused.
func Install(ctx context.Context, entry MarketplaceEntry, destRoot string) (string, error) {
	if entry.Name == "" || entry.Download == "" {
		return "", fmt.Errorf("plugin entry missing name/download")
	}
	dest := filepath.Join(destRoot, filepath.Base(entry.Name))
	if strings.ContainsAny(filepath.Base(entry.Name), `\/`) {
		return "", fmt.Errorf("invalid plugin name %q", entry.Name)
	}

	tmp, err := os.CreateTemp("", "bounty-plugin-*.zip")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	resp, err := getWithMirror(ctx, entry.Download)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", entry.Name, err)
	}
	defer resp.Body.Close()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, 64<<20)); err != nil {
		return "", fmt.Errorf("download %s: %w", entry.Name, err)
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if entry.SHA256 != "" && !strings.EqualFold(got, entry.SHA256) {
		return "", fmt.Errorf("sha256 mismatch for %s: got %s want %s", entry.Name, got, entry.SHA256)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return "", err
	}
	// Wipe any previous install of the same plugin before extracting.
	if _, err := os.Stat(dest); err == nil {
		if err := os.RemoveAll(dest); err != nil {
			return "", fmt.Errorf("clear previous install: %w", err)
		}
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	zr, err := zip.OpenReader(tmpName)
	if err != nil {
		return "", fmt.Errorf("open plugin zip: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		clean := filepath.Clean(filepath.FromSlash(f.Name))
		if clean == "." {
			continue
		}
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("zip-slip entry refused: %s", f.Name)
		}
		target := filepath.Join(dest, clean)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(filepath.Separator)) {
			return "", fmt.Errorf("zip entry escapes plugin dir: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return "", err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return "", err
		}
		out.Close()
		rc.Close()
	}
	return dest, nil
}

// Installed lists plugin directories (those containing plugin.toml) under
// destRoot.
func Installed(destRoot string) []string {
	entries, err := os.ReadDir(destRoot)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(destRoot, e.Name(), "plugin.toml")); err == nil {
			out = append(out, e.Name())
		}
	}
	return out
}
