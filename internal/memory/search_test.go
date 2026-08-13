package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func saveEntry(t *testing.T, root, name, desc, content string) {
	t.Helper()
	if err := NewRememberStore(root).Save(name, desc, content); err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
}

func TestLoadEntriesSkipsIndexAndParsesFrontmatter(t *testing.T) {
	root := t.TempDir()
	saveEntry(t, root, "naming-convention", "变量命名偏好", "用户偏好：变量命名一律使用 snake_case")
	saveEntry(t, root, "deploy-host", "部署服务器", "生产环境部署在 192.0.2.10")
	saveEntry(t, root, "meeting-time", "例会时间", "每周一上午十点开会")

	entries, err := LoadEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries=%d, want 3", len(entries))
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
		if e.Content == "" || e.Description == "" {
			t.Errorf("entry %s missing fields: %+v", e.Name, e)
		}
	}
	for _, want := range []string{"naming-convention", "deploy-host", "meeting-time"} {
		if !names[want] {
			t.Errorf("missing entry %s", want)
		}
	}
}

func TestRecentOrdersNewestFirst(t *testing.T) {
	root := t.TempDir()
	saveEntry(t, root, "first", "第一条", "最早写入")
	time.Sleep(5 * time.Millisecond)
	saveEntry(t, root, "second", "第二条", "最新写入")

	entries, err := Recent(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("recent=%d", len(entries))
	}
	if entries[0].Name != "second" {
		t.Errorf("newest first: got %s", entries[0].Name)
	}
}

func TestRecentLimit(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		saveEntry(t, root, n, "d", "c")
		time.Sleep(2 * time.Millisecond)
	}
	entries, err := Recent(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("limit: got %d", len(entries))
	}
}

func TestSearchChineseQuery(t *testing.T) {
	root := t.TempDir()
	saveEntry(t, root, "naming-convention", "变量命名偏好", "用户偏好：变量命名一律使用 snake_case，函数名用小写加下划线")
	saveEntry(t, root, "deploy-host", "部署服务器", "生产环境部署在 192.0.2.10，使用 nginx 反代")
	saveEntry(t, root, "meeting-time", "例会时间", "每周一上午十点开会")

	res, err := Search(root, "变量命名", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("no results")
	}
	if res[0].Name != "naming-convention" {
		t.Errorf("top hit = %s, want naming-convention", res[0].Name)
	}
}

func TestSearchEnglishToken(t *testing.T) {
	root := t.TempDir()
	saveEntry(t, root, "naming-convention", "变量命名偏好", "用户偏好 snake_case 命名")
	saveEntry(t, root, "deploy-host", "部署服务器", "生产环境部署在 192.0.2.10")

	res, err := Search(root, "snake_case", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 || res[0].Name != "naming-convention" {
		t.Fatalf("results=%+v", res)
	}
}

func TestSearchNoMatch(t *testing.T) {
	root := t.TempDir()
	saveEntry(t, root, "a", "d", "内容甲")

	res, err := Search(root, "完全无关的查询词", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("expected empty, got %+v", res)
	}
}

func TestSearchLimit(t *testing.T) {
	root := t.TempDir()
	saveEntry(t, root, "one", "部署", "内容")
	saveEntry(t, root, "two", "部署", "内容")
	saveEntry(t, root, "three", "部署", "内容")

	res, err := Search(root, "部署", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("limit: got %d", len(res))
	}
}

func TestParseEntryWithoutFrontmatter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".agent", "memory")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plain-note.md")
	if err := os.WriteFile(path, []byte("just raw content, no frontmatter"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := LoadEntries(filepath.Dir(filepath.Dir(dir)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%d", len(entries))
	}
	if entries[0].Name != "plain-note" {
		t.Errorf("name=%s", entries[0].Name)
	}
	if !strings.Contains(entries[0].Content, "just raw content") {
		t.Errorf("content=%q", entries[0].Content)
	}
}
