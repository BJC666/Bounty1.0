package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"bounty/internal/provider"
)

func TestMapBlocksNilForTextOnly(t *testing.T) {
	if got := mapBlocks(provider.Message{Role: "user", Content: "hi"}); got != nil {
		t.Fatalf("text-only must map to nil blocks, got %+v", got)
	}
}

func TestMapBlocksAnthropicWireFormat(t *testing.T) {
	m := provider.NewUserMessage("看图",
		provider.ImagePart{Data: "QUJD", MediaType: "image/png"})
	blocks := mapBlocks(m)
	if len(blocks) != 2 {
		t.Fatalf("blocks = %+v", blocks)
	}
	b, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"type":"text"`) || !strings.Contains(s, `"text":"看图"`) {
		t.Fatalf("text block missing: %s", s)
	}
	if !strings.Contains(s, `"type":"image"`) {
		t.Fatalf("image block missing: %s", s)
	}
	if !strings.Contains(s, `"source":{"type":"base64","media_type":"image/png","data":"QUJD"}`) {
		t.Fatalf("anthropic source shape missing: %s", s)
	}
}
