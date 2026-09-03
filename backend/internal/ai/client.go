// Package ai is the Anthropic Claude wrapper. No AI code ships in M1 — this is
// a stub whose call methods return ErrNotConfigured, wired into server.Deps so
// that main.go's wiring and the Deps struct do not churn when the real client
// is enabled at M4.
//
// At M4 this becomes one shared *anthropic.Client built once from the
// developer-held key (ANTHROPIC_API_KEY), a search_cards tool over the card
// mirror, and the validate-every-returned-card gate that reuses
// internal/deckrules. See docs/intent/ai-assist/.
//
// @spec AI-001
package ai

import "errors"

// ErrNotConfigured is returned by every Client method until the Anthropic
// client is enabled at M4.
var ErrNotConfigured = errors.New("ai: Anthropic client is not configured (no AI features ship before milestone M4)")

// Client is the AI facade. In M1 it holds no state.
type Client struct {
	enabled bool
}

// NewClient returns the M1 stub client.
func NewClient() *Client {
	return &Client{enabled: false}
}

// Enabled reports whether the real Anthropic client is wired up.
func (c *Client) Enabled() bool { return c.enabled }

// Suggest is the M4 "suggest & explain" entry point. It returns ErrNotConfigured
// in M1.
func (c *Client) Suggest() error {
	if !c.enabled {
		return ErrNotConfigured
	}
	return ErrNotConfigured
}
