// Package config loads runtime configuration from environment variables, with
// fail-fast validation: Load returns an error naming the first missing required
// variable, which cmd/api turns into log.Fatalf.
package config

import (
	"fmt"
	"os"

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

	// AnthropicAPIKey is the developer-held key for the AI features. Unused and
	// unvalidated in M1 (no AI code ships); becomes required at M4.
	AnthropicAPIKey string
}

// Load reads a .env file if present (local dev) then the real environment. Real
// environment variables always win.
func Load() (Config, error) {
	_ = godotenv.Load() // a missing .env is expected outside local dev

	cfg := Config{
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		FrontendURL:     os.Getenv("FRONTEND_URL"),
		DevAuth:         os.Getenv("DEV_AUTH") == "true",
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
	}

	// @spec PLATFORM-003
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required (see .env.example)")
	}
	if cfg.FrontendURL == "" {
		return Config{}, fmt.Errorf("FRONTEND_URL is required (see .env.example)")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
