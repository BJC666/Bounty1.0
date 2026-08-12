package validate

import (
	"strings"
	"testing"
)

// C3: 长度超过 MaxTitleLength 的标题必须被拒绝。
func TestValidateTitleRejectsOverMax(t *testing.T) {
	title := strings.Repeat("x", MaxTitleLength+1)
	if err := ValidateTitle(title); err == nil {
		t.Fatalf("expected error for %d-char title", len(title))
	}
}
