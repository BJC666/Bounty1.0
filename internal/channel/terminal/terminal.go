package terminal

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"bounty/internal/channel"
)

// TerminalChannel is a stdin/stdout REPL channel for local development.
type TerminalChannel struct {
	id      string
	handler channel.Handler
	active  bool
	cancel  context.CancelFunc
}

func New(id string, handler channel.Handler) *TerminalChannel {
	return &TerminalChannel{id: id, handler: handler}
}

func (t *TerminalChannel) ID() string        { return t.id }
func (t *TerminalChannel) Name() string      { return "Terminal REPL" }
func (t *TerminalChannel) IsConnected() bool { return t.active }

func (t *TerminalChannel) Start(ctx context.Context) error {
	t.active = true
	ctx, t.cancel = context.WithCancel(ctx)

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		fmt.Fprintln(os.Stderr, "[terminal] Channel active — type messages:")
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}
			if text == "/exit" {
				break
			}
			msg := channel.Message{
				ID:        fmt.Sprintf("term-%d", time.Now().UnixNano()),
				ChannelID: t.id,
				Text:      text,
			}
			if err := t.OnMessage(ctx, msg); err != nil {
				log.Printf("[terminal] message error: %v", err)
			}
		}
		t.active = false
	}()

	return nil
}

func (t *TerminalChannel) Stop(ctx context.Context) error {
	if t.cancel != nil {
		t.cancel()
	}
	t.active = false
	return nil
}

func (t *TerminalChannel) OnMessage(ctx context.Context, msg channel.Message) error {
	return t.handler.HandleMessage(ctx, msg)
}

func (t *TerminalChannel) Send(ctx context.Context, reply channel.Reply, target string) error {
	fmt.Println()
	fmt.Println(reply.Text)
	fmt.Println()
	return nil
}

func (t *TerminalChannel) HealthCheck(ctx context.Context) error {
	if !t.active {
		return fmt.Errorf("terminal channel not active")
	}
	return nil
}
