package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"bounty/internal/channel"
)

// TelegramChannel implements the channel.ChannelPlugin interface for Telegram.
type TelegramChannel struct {
	id      string
	token   string
	handler channel.Handler
	active  bool
	client  *http.Client
	offset  int64
	cancel  context.CancelFunc
}

// New creates a new TelegramChannel.
func New(id, token string, handler channel.Handler) *TelegramChannel {
	return &TelegramChannel{
		id:      id,
		token:   token,
		handler: handler,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *TelegramChannel) ID() string        { return t.id }
func (t *TelegramChannel) Name() string      { return "Telegram" }
func (t *TelegramChannel) IsConnected() bool { return t.active }

// Start begins polling the Telegram Bot API for updates.
func (t *TelegramChannel) Start(ctx context.Context) error {
	t.active = true
	ctx, t.cancel = context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.poll(ctx)
			}
		}
	}()

	log.Printf("[telegram] Bot started")
	return nil
}

// Stop cancels the polling loop and marks the channel as inactive.
func (t *TelegramChannel) Stop(ctx context.Context) error {
	if t.cancel != nil {
		t.cancel()
	}
	t.active = false
	return nil
}

func (t *TelegramChannel) poll(ctx context.Context) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=5", t.token, t.offset)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := t.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  struct {
				MessageID int64 `json:"message_id"`
				Chat      struct{ ID int64 `json:"id"` } `json:"chat"`
				Text      string `json:"text"`
				From      struct {
					ID        int64  `json:"id"`
					FirstName string `json:"first_name"`
				} `json:"from"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}
	if !result.OK {
		return
	}

	for _, update := range result.Result {
		if update.UpdateID >= t.offset {
			t.offset = update.UpdateID + 1
		}
		if update.Message.Text == "" {
			continue
		}

		msg := channel.Message{
			ID:        strconv.FormatInt(update.Message.MessageID, 10),
			ChannelID: t.id,
			UserID:    strconv.FormatInt(update.Message.From.ID, 10),
			UserName:  update.Message.From.FirstName,
			Text:      update.Message.Text,
		}
		if err := t.OnMessage(ctx, msg); err != nil {
			log.Printf("[telegram] message error: %v", err)
		}
	}
}

// OnMessage routes an inbound message to the channel handler.
func (t *TelegramChannel) OnMessage(ctx context.Context, msg channel.Message) error {
	return t.handler.HandleMessage(ctx, msg)
}

// Send posts a reply message to the specified Telegram chat.
func (t *TelegramChannel) Send(ctx context.Context, reply channel.Reply, target string) error {
	chatID, _ := strconv.ParseInt(target, 10, 64)
	body := map[string]interface{}{
		"chat_id": chatID,
		"text":    reply.Text,
	}
	if reply.Markdown != "" {
		body["text"] = reply.Markdown
		body["parse_mode"] = "Markdown"
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// HealthCheck verifies the bot token is valid by calling getMe.
func (t *TelegramChannel) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", t.token)
	resp, err := t.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("telegram health: %s", string(body))
	}
	return nil
}
