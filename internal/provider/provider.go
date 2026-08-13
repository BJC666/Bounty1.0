package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ImagePart carries one image as base64 (no data-URL prefix) plus its media
// type. Providers map it into their native content-block format.
type ImagePart struct {
	Data      string `json:"data"`
	MediaType string `json:"media_type"`
}

// ContentPart is one multimodal content block: text or image. A Message with
// a non-empty Parts slice is multimodal; otherwise Content carries the whole
// text (the legacy fast path, unchanged on the wire).
type ContentPart struct {
	Type  string     `json:"type"` // "text" | "image"
	Text  string     `json:"text,omitempty"`
	Image *ImagePart `json:"image,omitempty"`
}

type Message struct {
	Role      string        `json:"role"`
	Content   string        `json:"content,omitempty"`
	Parts     []ContentPart `json:"parts,omitempty"`
	ToolCalls []ToolCall    `json:"tool_calls,omitempty"`
	ToolName  string        `json:"name,omitempty"`
	ToolID    string        `json:"tool_call_id,omitempty"`
}

// NewUserMessage builds a user message; with images it becomes a content
// block message (text first, then images).
func NewUserMessage(text string, images ...ImagePart) Message {
	m := Message{Role: "user"}
	if len(images) == 0 {
		m.Content = text
		return m
	}
	if text != "" {
		m.Parts = append(m.Parts, ContentPart{Type: "text", Text: text})
	}
	for i := range images {
		m.Parts = append(m.Parts, ContentPart{Type: "image", Image: &images[i]})
	}
	return m
}

// maxImageBytes caps a single image to protect context and memory.
const maxImageBytes = 10 << 20 // 10 MiB

// LoadImageFile reads path, validates the image type, and returns a base64
// ImagePart. Accepted: png/jpeg/gif/webp.
func LoadImageFile(path string) (ImagePart, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ImagePart{}, fmt.Errorf("读取图片失败: %w", err)
	}
	if len(data) == 0 {
		return ImagePart{}, fmt.Errorf("图片为空: %s", path)
	}
	if len(data) > maxImageBytes {
		return ImagePart{}, fmt.Errorf("图片过大（%d 字节，上限 10MiB）: %s", len(data), path)
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		// DetectContentType only sniffs the header; fall back to extension
		// for uncommon-but-valid images (e.g. minimal PNG fixtures).
		switch strings.ToLower(filepath.Ext(path)) {
		case ".png":
			mime = "image/png"
		case ".jpg", ".jpeg":
			mime = "image/jpeg"
		case ".gif":
			mime = "image/gif"
		case ".webp":
			mime = "image/webp"
		default:
			return ImagePart{}, fmt.Errorf("不支持的图片类型: %s（仅支持 png/jpeg/gif/webp）", path)
		}
	}
	return ImagePart{
		Data:      base64.StdEncoding.EncodeToString(data),
		MediaType: mime,
	}, nil
}

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"arguments"`
}

type Delta struct {
	Role         string
	Content      string
	Reasoning    string
	ToolCalls    []ToolCallDelta
	FinishReason string
}

type ToolCallDelta struct {
	ID        string
	Name      string
	ArgsDelta string
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	CacheHit     bool
}

type StreamEvent struct {
	Delta *Delta
	Usage *Usage
	Done  bool
	Err   error
}

type StreamOpts struct {
	Temperature float64
	MaxTokens   int
	Effort      string
}

type Provider interface {
	Stream(ctx context.Context, messages []Message, tools []json.RawMessage, opts StreamOpts) (<-chan StreamEvent, error)
	// ContextWindow returns the provider's model context window in tokens
	// (0 = unknown). Used to derive compaction thresholds.
	ContextWindow() int
}
