package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bounty/internal/store"
)

type fakeTodoBackend struct {
	sessionID string
	todos     []store.Todo
}

func (f *fakeTodoBackend) ReplaceTodos(sessionID string, todos []store.Todo) error {
	f.sessionID = sessionID
	f.todos = todos
	return nil
}

func (f *fakeTodoBackend) LoadTodos(sessionID string) ([]store.Todo, error) {
	return f.todos, nil
}

func TestTodoWritePersists(t *testing.T) {
	be := &fakeTodoBackend{}
	tw := &TodoWriteTool{Store: be, SessionID: "sess-1"}
	args := json.RawMessage(`{"todos":[{"content":"读代码","status":"completed","activeForm":"读代码中"},{"content":"写修复","status":"in_progress","activeForm":"写修复中"},{"content":"跑测试","status":"pending","activeForm":""}]}`)
	out, err := tw.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if be.sessionID != "sess-1" {
		t.Errorf("session=%s", be.sessionID)
	}
	if len(be.todos) != 3 {
		t.Fatalf("todos=%d", len(be.todos))
	}
	if be.todos[1].Status != "in_progress" || be.todos[1].ActiveForm != "写修复中" {
		t.Errorf("persisted wrong: %+v", be.todos[1])
	}
	if !strings.Contains(out, "3 项") || !strings.Contains(out, "1 已完成") || !strings.Contains(out, "1 进行中") {
		t.Errorf("summary broken: %s", out)
	}
}

func TestTodoWriteNormalizesInvalidStatus(t *testing.T) {
	be := &fakeTodoBackend{}
	tw := &TodoWriteTool{Store: be, SessionID: "s"}
	out, err := tw.Execute(context.Background(), json.RawMessage(`{"todos":[{"content":"x","status":"banana","activeForm":""}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if be.todos[0].Status != "pending" {
		t.Errorf("status=%s", be.todos[0].Status)
	}
	_ = out
}

func TestTodoWriteWithoutBackendEchoes(t *testing.T) {
	tw := &TodoWriteTool{}
	out, err := tw.Execute(context.Background(), json.RawMessage(`{"todos":[{"content":"a","status":"pending","activeForm":""}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[pending] a") || !strings.Contains(out, "未连接持久层") {
		t.Errorf("echo broken: %s", out)
	}
}

func TestTodoWriteEmptyContentDropped(t *testing.T) {
	be := &fakeTodoBackend{}
	tw := &TodoWriteTool{Store: be, SessionID: "s"}
	if _, err := tw.Execute(context.Background(), json.RawMessage(`{"todos":[{"content":"  ","status":"pending","activeForm":""}]}`)); err != nil {
		t.Fatal(err)
	}
	if len(be.todos) != 0 {
		t.Fatalf("empty items must be dropped: %+v", be.todos)
	}
}
