package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestTodosRoundtrip(t *testing.T) {
	st := newTestStore(t)
	in := []Todo{
		{Content: "读代码", Status: "completed"},
		{Content: "写修复", Status: "in_progress", ActiveForm: "正在写修复"},
		{Content: "跑测试", Status: "pending"},
	}
	if err := st.ReplaceTodos("s1", in); err != nil {
		t.Fatal(err)
	}
	out, err := st.LoadTodos("s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Content != "读代码" || out[0].Status != "completed" {
		t.Errorf("order/status broken: %+v", out[0])
	}
	if out[1].ActiveForm != "正在写修复" {
		t.Errorf("active_form broken: %+v", out[1])
	}
}

func TestTodosReplaceOverwrites(t *testing.T) {
	st := newTestStore(t)
	if err := st.ReplaceTodos("s1", []Todo{{Content: "旧任务", Status: "pending"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceTodos("s1", []Todo{{Content: "新任务", Status: "in_progress"}}); err != nil {
		t.Fatal(err)
	}
	out, err := st.LoadTodos("s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Content != "新任务" {
		t.Fatalf("replace failed: %+v", out)
	}
}

func TestTodosSessionIsolation(t *testing.T) {
	st := newTestStore(t)
	if err := st.ReplaceTodos("a", []Todo{{Content: "A 的任务", Status: "pending"}}); err != nil {
		t.Fatal(err)
	}
	out, err := st.LoadTodos("b")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("session isolation broken: %+v", out)
	}
}

func TestTodosEmptySession(t *testing.T) {
	st := newTestStore(t)
	out, err := st.LoadTodos("none")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("want empty list, got %+v", out)
	}
}
