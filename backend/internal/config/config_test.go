package config

import (
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"PORT", "DATABASE_URL", "FRONTEND_URL", "DEV_AUTH", "ANTHROPIC_API_KEY",
		"TRUSTED_PROXY_COUNT", "AI_ENABLED", "AI_SUGGEST_DAILY_LIMIT",
		"AI_EXPLAIN_DAILY_LIMIT", "AI_MONTHLY_SPEND_USD",
	} {
		t.Setenv(k, "")
	}
}

// @spec PLATFORM-003
func TestLoad_RequiresDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("FRONTEND_URL", "http://localhost:3000")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when DATABASE_URL is unset")
	}
}

// @spec PLATFORM-003
func TestLoad_RequiresFrontendURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	_, err := Load()
	if err == nil {
		t.Fatal("expected an error when FRONTEND_URL is unset")
	}
	if !strings.Contains(err.Error(), "FRONTEND_URL") {
		t.Fatalf("expected the error to name FRONTEND_URL, got %v", err)
	}
}

// @spec PLATFORM-003
func TestLoadCardsync_DoesNotRequireFrontendURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://example")

	cfg, err := LoadCardsync()
	if err != nil {
		t.Fatalf("LoadCardsync: %v", err)
	}
	if cfg.DatabaseURL != "postgres://example" {
		t.Fatalf("DatabaseURL = %q, want postgres://example", cfg.DatabaseURL)
	}
}

// @spec PLATFORM-003
func TestLoadCardsync_RequiresDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	_, err := LoadCardsync()
	if err == nil {
		t.Fatal("expected an error when DATABASE_URL is unset")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected the error to name DATABASE_URL, got %v", err)
	}
}

func TestLoad_TrustedProxyCountFailSafe(t *testing.T) {
	// unset -> default 1; a valid non-negative int is honoured (0 included);
	// non-numeric or negative falls back to 1 so a bad value never widens trust.
	cases := []struct {
		raw  string
		want int
	}{
		{"", 1},
		{"2", 2},
		{"0", 0},
		{"garbage", 1},
		{"-3", 1},
	}
	for _, c := range cases {
		clearEnv(t)
		t.Setenv("DATABASE_URL", "postgres://example")
		t.Setenv("FRONTEND_URL", "http://localhost:3000")
		if c.raw != "" {
			t.Setenv("TRUSTED_PROXY_COUNT", c.raw)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load(%q): %v", c.raw, err)
		}
		if cfg.TrustedProxyCount != c.want {
			t.Fatalf("TRUSTED_PROXY_COUNT=%q -> %d, want %d", c.raw, cfg.TrustedProxyCount, c.want)
		}
	}
}

// @spec AI-002
func TestLoad_AIEnabledRequiresAnthropicKey(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")
	t.Setenv("AI_ENABLED", "true")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error when AI_ENABLED=true and ANTHROPIC_API_KEY is unset")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("expected the error to name ANTHROPIC_API_KEY, got %v", err)
	}

	// With the key present it loads.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with AI_ENABLED and a key: %v", err)
	}
	if !cfg.AIEnabled {
		t.Fatal("expected cfg.AIEnabled to be true")
	}
}

// @spec AI-002
func TestLoad_AIDisabledIgnoresMissingKey(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with AI disabled and no key must succeed: %v", err)
	}
	if cfg.AIEnabled {
		t.Fatal("cfg.AIEnabled must default to false")
	}
}

func TestLoad_AILimitDefaultsAndFailSafe(t *testing.T) {
	cases := []struct {
		suggest, explain, spend string
		wantSuggest, wantExpl   int
		wantSpend               float64
	}{
		{"", "", "", 20, 40, 50},
		{"5", "9", "12.5", 5, 9, 12.5},
		{"0", "-3", "-1", 20, 40, 50},        // non-positive int / negative float -> default
		{"garbage", "x", "nope", 20, 40, 50}, // unparseable -> default
		{"", "", "0", 20, 40, 0},             // an explicit 0 spend ceiling is honoured (disabled)
	}
	for _, c := range cases {
		clearEnv(t)
		t.Setenv("DATABASE_URL", "postgres://example")
		t.Setenv("FRONTEND_URL", "http://localhost:3000")
		if c.suggest != "" {
			t.Setenv("AI_SUGGEST_DAILY_LIMIT", c.suggest)
		}
		if c.explain != "" {
			t.Setenv("AI_EXPLAIN_DAILY_LIMIT", c.explain)
		}
		if c.spend != "" {
			t.Setenv("AI_MONTHLY_SPEND_USD", c.spend)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load(%+v): %v", c, err)
		}
		if cfg.AISuggestDailyLimit != c.wantSuggest || cfg.AIExplainDailyLimit != c.wantExpl || cfg.AIMonthlySpendUSD != c.wantSpend {
			t.Fatalf("Load(%+v) -> suggest=%d explain=%d spend=%v, want %d/%d/%v",
				c, cfg.AISuggestDailyLimit, cfg.AIExplainDailyLimit, cfg.AIMonthlySpendUSD, c.wantSuggest, c.wantExpl, c.wantSpend)
		}
	}
}

func TestLoad_DevAuthAndPortDefault(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")
	t.Setenv("DEV_AUTH", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.DevAuth {
		t.Fatal("expected cfg.DevAuth to be true")
	}
	if cfg.Port != "8080" {
		t.Fatalf("expected Port to default to 8080, got %q", cfg.Port)
	}
}
