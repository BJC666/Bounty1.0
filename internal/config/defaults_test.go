package config

import "testing"

func TestDefaultsNoCatchAllBash(t *testing.T) {
	d := Defaults()
	for _, p := range d.Permissions.Allow.BashPattern {
		if p == "*" {
			t.Fatal("default allow BashPattern must not contain catch-all '*'")
		}
	}
	if len(d.Permissions.Allow.BashPattern) == 0 {
		t.Fatal("expected a non-empty default bash allowlist")
	}
	// Dangerous commands must be covered by the deny list.
	deny := map[string]bool{}
	for _, p := range d.Permissions.Deny.BashPattern {
		deny[p] = true
	}
	for _, dangerous := range []string{"rm *", "rm -rf *", "sudo *", "del *", "format *"} {
		if !deny[dangerous] {
			t.Errorf("default deny list missing %q", dangerous)
		}
	}
}