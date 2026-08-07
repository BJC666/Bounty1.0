package memory

import (
	"strings"
	"testing"
)

func TestScanSensitive(t *testing.T) {
	cases := []string{
		"the key is sk-abcdefghijklmnopqrstuvwxyz1234",
		"AWS credentials AKIAIOSFODNN7EXAMPLE were used",
		"github token ghp_0123456789abcdefghijklmnopqrstuv",
		"slack xoxb-1234567890-abcdefghij",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA",
		"password: hunter2",
		"api_key = sk-abcdefghijklmnopqrstuvwxyz1234",
	}
	for _, c := range cases {
		if len(ScanSensitive(c)) == 0 {
			t.Errorf("ScanSensitive(%q) = none, want a hit", c)
		}
	}
}

func TestScanSensitiveClean(t *testing.T) {
	cases := []string{
		"summarize the quarterly report",
		"the service was temporarily unavailable",
		"please refactor the storage layer",
	}
	for _, c := range cases {
		if hits := ScanSensitive(c); len(hits) != 0 {
			t.Errorf("ScanSensitive(%q) = %v, want clean", c, hits)
		}
	}
}

func TestRedactSensitive(t *testing.T) {
	in := "key=sk-abcdefghijklmnopqrstuvwxyz1234 and password: hunter2"
	out := RedactSensitive(in)
	if strings.Contains(out, "sk-abcdefghijklmnopqrstuvwxyz1234") || strings.Contains(out, "hunter2") {
		t.Errorf("RedactSensitive left secrets in output: %q", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Errorf("RedactSensitive did not mark redaction: %q", out)
	}
}

func TestRedactSensitiveIdempotent(t *testing.T) {
	in := "token sk-abcdefghijklmnopqrstuvwxyz1234"
	once := RedactSensitive(in)
	twice := RedactSensitive(once)
	if once != twice {
		t.Errorf("redaction not idempotent: %q vs %q", once, twice)
	}
}
