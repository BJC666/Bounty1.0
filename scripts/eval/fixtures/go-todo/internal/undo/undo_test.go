package undo

import "testing"

// B4: 后进先出的字符串栈。
func TestStackLIFO(t *testing.T) {
	st := New()
	st.Push("a")
	st.Push("b")
	if got, ok := st.Pop(); !ok || got != "b" {
		t.Fatalf("Pop = %q, %v; want b", got, ok)
	}
	if got, ok := st.Pop(); !ok || got != "a" {
		t.Fatalf("Pop = %q, %v; want a", got, ok)
	}
	if _, ok := st.Pop(); ok {
		t.Fatal("Pop on empty stack must report false")
	}
}
