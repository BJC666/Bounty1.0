package checkpoint

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found on PATH")
	}
}

func writeTestFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// treeSnapshot collects relpath -> content for every file in the tree.
func treeSnapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			out[filepath.ToSlash(rel)+"/"] = nil
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestGitStoreRollbackFiftyFiles is the roadmap acceptance test: corrupt 50
// files, add 20 untracked files, delete 10 files, then roll back with one call
// and verify the tree is byte-identical to the baseline.
func TestGitStoreRollbackFiftyFiles(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	for i := 0; i < 50; i++ {
		writeTestFile(t, ws, fmt.Sprintf("files/f%02d.txt", i), []byte(fmt.Sprintf("original-%02d", i)))
	}
	st, err := NewGit(ws, filepath.Join(t.TempDir(), "shadow"))
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	st.BeginTurn("baseline", 0)
	if err := st.SaveTurn("baseline", 0); err != nil {
		t.Fatalf("SaveTurn(0): %v", err)
	}
	baseline := treeSnapshot(t, ws)

	// Disaster: modify all 50, add 20 untracked, delete 10.
	for i := 0; i < 50; i++ {
		p := filepath.Join(ws, fmt.Sprintf("files/f%02d.txt", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("corrupted-%02d", i)), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 20; i++ {
		writeTestFile(t, ws, fmt.Sprintf("junk/j%02d.txt", i), []byte("junk-data"))
	}
	for i := 0; i < 10; i++ {
		if err := os.Remove(filepath.Join(ws, fmt.Sprintf("files/f%02d.txt", i))); err != nil {
			t.Fatal(err)
		}
	}
	st.BeginTurn("disaster", 1)
	if err := st.SaveTurn("disaster", 1); err != nil {
		t.Fatalf("SaveTurn(1): %v", err)
	}

	if err := st.RestoreCheckpoint(0); err != nil {
		t.Fatalf("RestoreCheckpoint(0): %v", err)
	}

	got := treeSnapshot(t, ws)
	if len(got) != len(baseline) {
		t.Fatalf("tree size after restore = %d, want %d (baseline)", len(got), len(baseline))
	}
	for name, want := range baseline {
		data, ok := got[name]
		if !ok {
			t.Fatalf("path %q missing after restore", name)
		}
		if !bytes.Equal(data, want) {
			t.Fatalf("path %q content differs after restore (len %d vs %d)", name, len(data), len(want))
		}
	}
}

// TestGitStoreRestoreIntermediate verifies rollback to a middle checkpoint
// keeps the edits of that checkpoint and drops later ones.
func TestGitStoreRestoreIntermediate(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	writeTestFile(t, ws, "a.txt", []byte("v0"))
	st, err := NewGit(ws, filepath.Join(t.TempDir(), "shadow"))
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	st.BeginTurn("m0", 0)
	if err := st.SaveTurn("m0", 0); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, ws, "a.txt", []byte("v1"))
	st.BeginTurn("m1", 1)
	if err := st.SaveTurn("m1", 1); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, ws, "a.txt", []byte("v2"))
	writeTestFile(t, ws, "b.txt", []byte("late"))
	st.BeginTurn("m2", 2)
	if err := st.SaveTurn("m2", 2); err != nil {
		t.Fatal(err)
	}

	if err := st.RestoreCheckpoint(1); err != nil {
		t.Fatalf("RestoreCheckpoint(1): %v", err)
	}
	data, err := os.ReadFile(filepath.Join(ws, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v1" {
		t.Fatalf("a.txt = %q, want v1", data)
	}
	if _, err := os.Stat(filepath.Join(ws, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("b.txt should be removed by rollback, stat err = %v", err)
	}
}

// TestGitStoreBinaryAndChineseNames verifies byte-exact round trip for binary
// payloads and non-ASCII paths on Windows.
func TestGitStoreBinaryAndChineseNames(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	bin := []byte{0x00, 0xff, 0x01, 0x80, 0x7f, 0x0d, 0x0a, 0x1a}
	writeTestFile(t, ws, filepath.Join("中文 目录", "数据文件.bin"), bin)
	writeTestFile(t, ws, "笔记.md", []byte("原始中文内容\r\n第二行"))
	st, err := NewGit(ws, filepath.Join(t.TempDir(), "shadow"))
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	st.BeginTurn("t0", 0)
	if err := st.SaveTurn("t0", 0); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, ws, filepath.Join("中文 目录", "数据文件.bin"), []byte("corrupted"))
	writeTestFile(t, ws, "笔记.md", []byte("被改坏"))
	st.BeginTurn("t1", 1)
	if err := st.SaveTurn("t1", 1); err != nil {
		t.Fatal(err)
	}

	if err := st.RestoreCheckpoint(0); err != nil {
		t.Fatalf("RestoreCheckpoint(0): %v", err)
	}
	gotBin, err := os.ReadFile(filepath.Join(ws, "中文 目录", "数据文件.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBin, bin) {
		t.Fatalf("binary content differs: got %v want %v", gotBin, bin)
	}
	gotTxt, err := os.ReadFile(filepath.Join(ws, "笔记.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotTxt) != "原始中文内容\r\n第二行" {
		t.Fatalf("text content differs: %q", gotTxt)
	}
}

// TestGitStoreFileDirSwap: a tracked file replaced by a directory of junk must
// come back as a file after rollback.
func TestGitStoreFileDirSwap(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	writeTestFile(t, ws, "x.txt", []byte("tracked-file"))
	st, err := NewGit(ws, filepath.Join(t.TempDir(), "shadow"))
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	st.BeginTurn("t0", 0)
	if err := st.SaveTurn("t0", 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(ws, "x.txt")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, ws, filepath.Join("x.txt", "inner.txt"), []byte("dir-junk"))
	st.BeginTurn("t1", 1)
	if err := st.SaveTurn("t1", 1); err != nil {
		t.Fatal(err)
	}

	if err := st.RestoreCheckpoint(0); err != nil {
		t.Fatalf("RestoreCheckpoint(0): %v", err)
	}
	data, err := os.ReadFile(filepath.Join(ws, "x.txt"))
	if err != nil {
		t.Fatalf("x.txt should be a file again: %v", err)
	}
	if string(data) != "tracked-file" {
		t.Fatalf("x.txt = %q, want tracked-file", data)
	}
}

// TestGitStoreKeepsRealGitDirUntouched: the workspace's own .git directory must
// survive both checkpoint commits and rollbacks.
func TestGitStoreKeepsRealGitDirUntouched(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	writeTestFile(t, ws, filepath.Join(".git", "config"), []byte("[core]\n"))
	writeTestFile(t, ws, "f.txt", []byte("keep-me"))
	st, err := NewGit(ws, filepath.Join(t.TempDir(), "shadow"))
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	st.BeginTurn("t0", 0)
	if err := st.SaveTurn("t0", 0); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, ws, filepath.Join(".git", "new-object.txt"), []byte("junk"))
	writeTestFile(t, ws, "f.txt", []byte("changed"))
	st.BeginTurn("t1", 1)
	if err := st.SaveTurn("t1", 1); err != nil {
		t.Fatal(err)
	}
	if err := st.RestoreCheckpoint(0); err != nil {
		t.Fatalf("RestoreCheckpoint(0): %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".git", "config")); err != nil {
		t.Fatalf(".git/config must survive: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(ws, "f.txt")); string(data) != "keep-me" {
		t.Fatalf("f.txt = %q, want keep-me", data)
	}
}

func TestGitStoreListCheckpoints(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	writeTestFile(t, ws, "a.txt", []byte("v0"))
	st, err := NewGit(ws, filepath.Join(t.TempDir(), "shadow"))
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	st.BeginTurn("first prompt", 0)
	if err := st.SaveTurn("first prompt", 0); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, ws, "a.txt", []byte("v1"))
	st.BeginTurn("second prompt", 1)
	if err := st.SaveTurn("second prompt", 1); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListCheckpoints()
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	if list[0].MsgIndex != 0 || list[1].MsgIndex != 1 {
		t.Fatalf("order wrong: %+v", list)
	}
	if list[0].Prompt != "first prompt" || list[1].Prompt != "second prompt" {
		t.Fatalf("prompts wrong: %+v", list)
	}
}

func TestGitStoreRestoreUnknownMessage(t *testing.T) {
	requireGit(t)
	ws := t.TempDir()
	writeTestFile(t, ws, "a.txt", []byte("v0"))
	st, err := NewGit(ws, filepath.Join(t.TempDir(), "shadow"))
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	st.BeginTurn("t0", 0)
	if err := st.SaveTurn("t0", 0); err != nil {
		t.Fatal(err)
	}
	if err := st.RestoreCheckpoint(99); err == nil || !strings.Contains(err.Error(), "msg-99") {
		t.Fatalf("RestoreCheckpoint(99) = %v, want error mentioning msg-99", err)
	}
}

func TestNewGitEmptyWorkspace(t *testing.T) {
	if _, err := NewGit("", filepath.Join(t.TempDir(), "s")); err == nil {
		t.Fatal("NewGit with empty workspace must fail")
	}
}
