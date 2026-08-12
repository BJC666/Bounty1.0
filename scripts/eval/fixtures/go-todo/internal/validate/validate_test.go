package validate

import "testing"

func TestValidateTitleAcceptsNormalTitle(t *testing.T) {
	if err := ValidateTitle("write report"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTitleRejectsEmpty(t *testing.T) {
	if err := ValidateTitle(""); err == nil {
		t.Fatal("expected error for empty title")
	}
}
