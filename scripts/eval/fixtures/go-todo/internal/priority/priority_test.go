package priority

import "testing"

// B2: Normalize 把中文优先级映射为英文。
func TestNormalizeChinese(t *testing.T) {
	cases := map[string]string{"高": "high", "中": "medium", "低": "low"}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizePassThrough(t *testing.T) {
	if got := Normalize("high"); got != "high" {
		t.Fatalf("Normalize(high) = %q", got)
	}
}
