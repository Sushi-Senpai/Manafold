// Package config loads runtime configuration from environment variables, with
// fail-fast validation: Load returns an error naming the first missing required
// variable, which cmd/api turns into log.Fatalf.
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds every setting the server needs at startup. One struct, so there
// is a single place that knows what the app depends on.
type Config struct {
	Port        string
	DatabaseURL string
	FrontendURL string

	// DevAuth, when true, signs every /api request in as one fixed local-dev
	// user (internal/middleware.DevAuth) instead of a real session. Off by
	// default so it can never silently activate in a deployed environment. M1
	// runs on this stub only; the real email + password login flow is M3.
	DevAuth bool

	// TrustedProxyCount is how many reverse proxies in front of the API append
	// the client address to X-Forwarded-For. The per-IP auth rate limiter
	// (ACCT-017) reads its key that many hops from the right of the chain, so a
	// caller cannot mint a fresh bucket by prepending a forged entry. Defaults
	// to 1 (Render's own edge). It MUST be set to the deployed stack's true hop
	// count — 2 if the Next.js rewrite also appends — and verified before the
	// credential-stuffing / signup-spam guard is relied on in production.
	TrustedProxyCount int

	// AnthropicAPIKey is the developer-held key for the AI features (AI-003).
	// Required only when AIEnabled is true; Load fails fast naming it in that
	// case.
	AnthropicAPIKey string

	// AIEnabled turns the M4 AI features on. Default false, so dev and CI build
	// and boot with no key; when true the server builds the real Anthropic
	// assistant and requires AnthropicAPIKey (AI-002). When false the wired
	// assistant is ai.Disabled() and every AI endpoint answers 503.
	AIEnabled bool

	// AISuggestDailyLimit / AIExplainDailyLimit are the per-user, per-day call
	// caps for each AI feature (AI-031). A user at the cap gets 429 until the
	// calendar day rolls over.
	AISuggestDailyLimit int
	AIExplainDailyLimit int

	// AIMonthlySpendUSD is the global month-to-date estimated-spend ceiling
	// across all users (AI-032). At or above it, every AI endpoint answers 503.
	// 0 disables the check.
	AIMonthlySpendUSD float64
}

// Load reads a .env file if present (local dev) then the real environment. Real
// environment variables always win. It is the API server's config path and
// requires both DATABASE_URL and FRONTEND_URL.
func Load() (Config, error) {
	cfg := readEnv()

	// @spec PLATFORM-003
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required (see .env.example)")
	}
	if cfg.FrontendURL == "" {
		return Config{}, fmt.Errorf("FRONTEND_URL is required (see .env.example)")
	}

	// @spec AI-002
	if cfg.AIEnabled && cfg.AnthropicAPIKey == "" {
		return Config{}, fmt.Errorf("ANTHROPIC_API_KEY is required when AI_ENABLED=true (see .env.example)")
	}

	return cfg, nil
}

// LoadCardsync is the minimal config path for cmd/cardsync, the standalone
// Scryfall ingestion job (PLATFORM-006). It has no HTTP surface and only needs
// the database, so it validates DATABASE_URL and deliberately does not require
// FRONTEND_URL, leaving the PLATFORM-003 fail-fast for the API server's Load.
func LoadCardsync() (Config, error) {
	cfg := readEnv()

	// @spec PLATFORM-003
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required (see .env.example)")
	}

	return cfg, nil
}

// readEnv loads a .env file if present (local dev) then the real environment and
// returns the populated Config without validation. Real environment variables
// always win.
func readEnv() Config {
	_ = godotenv.Load() // a missing .env is expected outside local dev

	return Config{
		Port:                getEnv("PORT", "8080"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		FrontendURL:         os.Getenv("FRONTEND_URL"),
		DevAuth:             os.Getenv("DEV_AUTH") == "true",
		AnthropicAPIKey:     os.Getenv("ANTHROPIC_API_KEY"),
		AIEnabled:           os.Getenv("AI_ENABLED") == "true",
		AISuggestDailyLimit: getEnvPosInt("AI_SUGGEST_DAILY_LIMIT", 20),
		AIExplainDailyLimit: getEnvPosInt("AI_EXPLAIN_DAILY_LIMIT", 40),
		AIMonthlySpendUSD:   getEnvNonNegFloat("AI_MONTHLY_SPEND_USD", 50),
		TrustedProxyCount:   getEnvNonNegInt("TRUSTED_PROXY_COUNT", 1),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvNonNegInt reads a non-negative integer env var, returning fallback when
// it is unset, empty, non-numeric, or negative — the fail-safe direction for
// TRUSTED_PROXY_COUNT, where a bad value must not silently widen trust.
func getEnvNonNegInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

// getEnvPosInt reads a strictly-positive integer env var, returning fallback
// when it is unset, empty, non-numeric, or not positive. Used for the AI daily
// call limits, where 0 or a garbled value should fall back to the default
// rather than lock the feature out or read as "unlimited".
func getEnvPosInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// getEnvNonNegFloat reads a non-negative float env var, returning fallback when
// it is unset, empty, non-numeric, or negative. Used for AI_MONTHLY_SPEND_USD,
// where an explicit 0 is meaningful (disables the ceiling) and so is honoured.
func getEnvNonNegFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return fallback
	}
	return f
}
