package ai

import (
	"errors"
	"testing"
)

// @spec AI-001
func TestStubClient_ReturnsNotConfigured(t *testing.T) {
	c := NewClient()
	if c.Enabled() {
		t.Fatalf("M1 stub client must not report itself enabled")
	}
	if err := c.Suggest(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Suggest() = %v, want ErrNotConfigured", err)
	}
}
