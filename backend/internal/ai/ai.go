// Package ai is the Anthropic Claude wrapper for Manafold's "suggest & explain"
// and single-card fit-blurb features (M4). It owns only the model conversation:
// the HTTP handler owns the database and the anti-hallucination gate that
// re-validates every card the model names, so this package never imports the
// database layer and stays unit-testable with a fake Assistant.
//
// A build turns the features on with AI_ENABLED=true; when it is unset the
// wired Assistant is Disabled(), whose methods return ErrNotConfigured and whose
// Enabled() is false, so the app still builds and boots with no key present.
//
// @spec AI-001, AI-003, AI-004, AI-050
package ai

import (
	"context"
	"errors"
)

// ErrNotConfigured is returned by the Disabled() assistant's call methods.
var ErrNotConfigured = errors.New("ai: Anthropic assistant is not configured (set AI_ENABLED=true and ANTHROPIC_API_KEY)")

// Model IDs, one per feature — the cheap tiers the design assigns (AI-004).
const (
	suggestModel = "claude-sonnet-5"
	explainModel = "claude-haiku-4-5"
	// curateModel serves the deck wizard's rounds; it is the suggest model
	// (AI-051).
	curateModel = suggestModel
)

// CardRef is the minimal card shape the assistant reasons over: enough for the
// model to judge fit, nothing it should not need.
type CardRef struct {
	Name          string   `json:"name"`
	ManaCost      string   `json:"mana_cost,omitempty"`
	TypeLine      string   `json:"type_line,omitempty"`
	OracleText    string   `json:"oracle_text,omitempty"`
	ColorIdentity []string `json:"color_identity,omitempty"`
	EdhrecRank    *int     `json:"edhrec_rank,omitempty"`
}

// SearchFunc runs a mirror search already filtered to the deck's colour
// identity and to non-banned cards (AI-012). The handler builds it over the
// card table; the assistant only calls it.
type SearchFunc func(ctx context.Context, query string) ([]CardRef, error)

// Usage is the token accounting the handler records after a successful call.
type Usage struct {
	Model        string
	InputTokens  int64
	OutputTokens int64
	CostMicros   int64
}

// SuggestRequest is everything the suggestion loop needs. Candidates is the
// deterministic edhrec_rank-ordered pool; Search lets the model explore beyond
// it within the same colour-identity / legality filter.
type SuggestRequest struct {
	CommanderName string
	CommanderText string
	DeckIdentity  []string
	InDeck        []string
	Candidates    []CardRef
	Want          int
	Search        SearchFunc
}

// Suggestion is one model-proposed card before the gate runs.
type Suggestion struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// SuggestResult is the raw model output plus usage; the handler runs the gate
// over Suggestions before responding.
type SuggestResult struct {
	Suggestions []Suggestion
	Usage       Usage
}

// ExplainRequest is the single-card blurb input.
type ExplainRequest struct {
	CommanderName string
	CommanderText string
	CardName      string
	CardText      string
	DeckIdentity  []string
}

// ExplainResult is the prose plus usage.
type ExplainResult struct {
	Text  string
	Usage Usage
}

// PoolCard is one candidate the deck wizard offers the model: a card the
// backend has already proven legal, in-bracket and within the player's
// restrictions, plus the facts the model needs to choose between candidates.
type PoolCard struct {
	CardRef
	ProducedMana []string
	GameChanger  bool
	PriceUSD     *float64
	// InclusionPct and SynergyPct are set only when the pool came from stored
	// synergy data: the share of sampled decks playing the card, and that share
	// minus the colour-identity baseline's, in percentage points.
	InclusionPct *int
	SynergyPct   *int
}

// CurateRequest is one deck-wizard round. Pool is the whole menu: the model may
// pick only from it, and the handler enforces that (AI-052).
type CurateRequest struct {
	Step         string
	StepGuidance string

	CommanderName string
	CommanderText string
	DeckIdentity  []string

	Bracket         int
	BracketGuidance string
	// GameChangersLeft is how many more Game Changers the bracket admits; nil
	// means unlimited.
	GameChangersLeft *int

	// Notes is the player's free text. It is untrusted and steers curation only.
	Notes string

	InDeck []string
	Pool   []PoolCard
	Want   int
}

// CurateResult is the model's summary and picks plus usage; the handler runs the
// gate, pool membership and bracket admission over Picks before responding.
type CurateResult struct {
	Summary string
	Picks   []Suggestion
	Usage   Usage
}

// Assistant is the model-facing surface. server.Deps carries one; handlers call
// it and never construct one.
type Assistant interface {
	// Enabled reports whether real model calls will be made. When false, Suggest
	// and Explain return ErrNotConfigured and the endpoints answer 503.
	Enabled() bool
	// Suggest curates req.Candidates (and anything req.Search turns up) into up
	// to req.Want cards with a one-sentence rationale each.
	Suggest(ctx context.Context, req SuggestRequest) (SuggestResult, error)
	// Explain returns two or three sentences on why one card fits the deck.
	Explain(ctx context.Context, req ExplainRequest) (ExplainResult, error)
	// Curate picks up to req.Want cards from req.Pool for one deck-wizard round,
	// with a one-sentence reason each and a short summary for the player.
	Curate(ctx context.Context, req CurateRequest) (CurateResult, error)
}

// disabled is the no-key Assistant.
type disabled struct{}

// Disabled returns the Assistant wired when AI_ENABLED is unset.
func Disabled() Assistant { return disabled{} }

func (disabled) Enabled() bool { return false }

func (disabled) Suggest(context.Context, SuggestRequest) (SuggestResult, error) {
	return SuggestResult{}, ErrNotConfigured
}

func (disabled) Explain(context.Context, ExplainRequest) (ExplainResult, error) {
	return ExplainResult{}, ErrNotConfigured
}

func (disabled) Curate(context.Context, CurateRequest) (CurateResult, error) {
	return CurateResult{}, ErrNotConfigured
}
