package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"bounty/internal/provider"
)

func TestMapContentTextOnly(t *testing.T) {
	got := mapContent(provider.Message{Role: "user", Content: "hi"})
	if got != "hi" {
		t.Fatalf("text-only content must stay a string, got %T %v", got, got)
	}
}

func TestMapContentBlocksWireFormat(t *testing.T) {
	m := provider.NewUserMessage("看图",
		provider.ImagePart{Data: "QUJD", MediaType: "image/png"})
	cm := chatMessage{Role: "user", Content: mapContent(m)}
	b, err := json.Marshal(cm)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"type":"text"`) || !strings.Contains(s, `"text":"看图"`) {
		t.Fatalf("text block missing: %s", s)
	}
	if !strings.Contains(s, `"type":"image_url"`) {
		t.Fatalf("image block missing: %s", s)
	}
	if !strings.Contains(s, `"url":"data:image/png;base64,QUJD"`) {
		t.Fatalf("data URL missing: %s", s)
	}
}
