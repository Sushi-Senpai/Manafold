package ai

import (
	"context"
	"errors"
	"testing"
)

// @spec AI-001
func TestDisabled_ReturnsNotConfigured(t *testing.T) {
	a := Disabled()
	if a.Enabled() {
		t.Fatal("Disabled() assistant must not report itself enabled")
	}
	if _, err := a.Suggest(context.Background(), SuggestRequest{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Suggest() = %v, want ErrNotConfigured", err)
	}
	if _, err := a.Explain(context.Background(), ExplainRequest{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Explain() = %v, want ErrNotConfigured", err)
	}
}

// @spec AI-003, AI-004
func TestNewAnthropic_IsEnabled(t *testing.T) {
	a := NewAnthropic("sk-ant-test-key")
	if !a.Enabled() {
		t.Fatal("NewAnthropic assistant must report itself enabled")
	}
	// A second Suggest/Explain reuses the same client — nothing here constructs
	// a client per call. We cannot exercise the network, but the type must
	// satisfy Assistant and hold one client.
	if _, ok := a.(*anthropicAssistant); !ok {
		t.Fatalf("NewAnthropic returned %T, want *anthropicAssistant", a)
	}
}

// @spec AI-004
func TestEstimateCost_PerFeatureModelPricing(t *testing.T) {
	// One million input + one million output tokens, so the result is just the
	// per-MTok rates summed.
	suggest := estimateCostMicros(suggestModel, 1_000_000, 1_000_000)
	explain := estimateCostMicros(explainModel, 1_000_000, 1_000_000)

	// claude-sonnet-5 is $2 + $10 per MTok; claude-haiku-4-5 is $1 + $5.
	if suggest != 12_000_000 {
		t.Fatalf("suggest model (%s) cost = %d micro-USD, want 12_000_000", suggestModel, suggest)
	}
	if explain != 6_000_000 {
		t.Fatalf("explain model (%s) cost = %d micro-USD, want 6_000_000", explainModel, explain)
	}
	if explain >= suggest {
		t.Fatalf("the single-card blurb model must be the cheaper tier: explain=%d suggest=%d", explain, suggest)
	}
}

// An unknown model is charged at the Sonnet rate, never under-charged.
func TestEstimateCost_UnknownModelFallsBackToSonnet(t *testing.T) {
	got := estimateCostMicros("claude-made-up-9", 1_000_000, 0)
	if got != 2_000_000 {
		t.Fatalf("unknown-model input cost = %d, want the 2_000_000 Sonnet rate", got)
	}
}

// capSuggestions trims to the target count and drops blank names.
func TestCapSuggestions(t *testing.T) {
	in := []Suggestion{{Name: "A"}, {Name: " "}, {Name: "B"}, {Name: "C"}}
	got := capSuggestions(in, 2)
	if len(got) != 2 || got[0].Name != "A" || got[1].Name != "B" {
		t.Fatalf("capSuggestions = %+v, want [A B]", got)
	}
}
