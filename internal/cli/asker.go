package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// TerminalAsker prompts the user for approval on stdin/stdout. It satisfies
// agent.Asker so that Ask decisions (permission gates, yolo guardian
// escalations) pause for real user input instead of failing closed.
type TerminalAsker struct{}

func (TerminalAsker) Ask(ctx context.Context, question string, options []string) (string, error) {
	fmt.Fprintf(os.Stderr, "\n⚠️  %s\n", question)
	fmt.Fprintf(os.Stderr, "Options: %s\n> ", strings.Join(options, ", "))
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no input")
	}
	return strings.TrimSpace(sc.Text()), nil
}

// pendingAsk is an in-flight permission question shown as an overlay.
type pendingAsk struct {
	question string
	options  []string
	reply    chan string
}

// tuiAsker is the bubbletea-integrated permission dialog. Ask blocks the agent
// goroutine while the TUI renders the question overlay on each tick; digit
// keys answer it and Esc cancels (empty answer = deny).
type tuiAsker struct {
	mu      sync.Mutex
	pending *pendingAsk
}

func (a *tuiAsker) Ask(ctx context.Context, question string, options []string) (string, error) {
	reply := make(chan string, 1)
	a.mu.Lock()
	a.pending = &pendingAsk{question: question, options: options, reply: reply}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.pending != nil && a.pending.reply == reply {
			a.pending = nil
		}
		a.mu.Unlock()
	}()
	select {
	case ans := <-reply:
		return ans, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Pending returns the in-flight question, or nil.
func (a *tuiAsker) Pending() *pendingAsk {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pending
}

// Answer replies with options[idx]; returns false when nothing is pending or
// the index is out of range.
func (a *tuiAsker) Answer(idx int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil || idx < 0 || idx >= len(a.pending.options) {
		return false
	}
	select {
	case a.pending.reply <- a.pending.options[idx]:
	default:
	}
	a.pending = nil
	return true
}

// Cancel replies with an empty string (deny) and clears the pending question.
func (a *tuiAsker) Cancel() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil {
		return false
	}
	select {
	case a.pending.reply <- "":
	default:
	}
	a.pending = nil
	return true
}
