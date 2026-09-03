package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// maxToolIterations caps the suggest loop's model round-trips before a final
// forced submit_suggestions call. Small on purpose: the pool is already good,
// the tool is for targeted lookups, not open-ended research.
const maxToolIterations = 4

// errNoSuggestions is returned when the model finishes without proposing any
// card; the handler turns it into a 502.
var errNoSuggestions = errors.New("ai: model returned no suggestions")

type anthropicAssistant struct {
	client anthropic.Client
}

// NewAnthropic builds the real assistant: one shared client reused for every
// request (developer-keyed, so there is a package-level client — unlike a BYOK
// design).
func NewAnthropic(apiKey string) Assistant {
	return &anthropicAssistant{client: anthropic.NewClient(option.WithAPIKey(apiKey))}
}

func (a *anthropicAssistant) Enabled() bool { return true }

// @spec AI-020
func (a *anthropicAssistant) Suggest(ctx context.Context, req SuggestRequest) (SuggestResult, error) {
	want := req.Want
	if want <= 0 || want > 10 {
		want = 10
	}

	searchTool := anthropic.ToolParam{
		Name: "search_cards",
		Description: anthropic.String("Search Manafold's card mirror with Scryfall-style syntax " +
			"(for example `t:instant cmc<=2`, `o:\"draw a card\"`, `id:wu`). Results are already " +
			"restricted to the deck's colour identity and to Commander-legal cards. Use it to look " +
			"beyond the starting candidate list for specific effects."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"query": map[string]any{"type": "string", "description": "the search query"},
			},
			Required: []string{"query"},
		},
	}
	submitTool := anthropic.ToolParam{
		Name: "submit_suggestions",
		Description: anthropic.String("Call exactly once with your final ranked list. Every name must be " +
			"a real Magic card within the deck's colour identity that is not already in the deck."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"suggestions": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":   map[string]any{"type": "string"},
							"reason": map[string]any{"type": "string", "description": "one sentence on why it fits"},
						},
						"required": []string{"name", "reason"},
					},
				},
			},
			Required: []string{"suggestions"},
		},
	}
	tools := []anthropic.ToolUnionParam{{OfTool: &searchTool}, {OfTool: &submitTool}}

	system := "You are a Magic: The Gathering Commander deckbuilding assistant. Recommend cards that " +
		"improve the given deck. Every card you name must be a real Magic card, must fall within the " +
		"deck's colour identity, and must not already be in the deck; never suggest basic lands. Prefer " +
		"the provided candidate list, which is ordered by how commonly the card sees play in this kind " +
		"of deck; use search_cards for specific effects it is missing. When finished, call " +
		"submit_suggestions exactly once with " + fmt.Sprintf("%d", want) + " or fewer cards, each with a one-sentence reason."

	messages := []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(suggestPrompt(req)))}

	usage := Usage{Model: suggestModel}
	// finalize stamps the running token totals with their estimated cost. Every
	// return path — success, parse failure, no-submit_suggestions — sends the
	// Usage back so the handler can meter a call that reached the model.
	finalize := func() Usage {
		usage.CostMicros = estimateCostMicros(usage.Model, usage.InputTokens, usage.OutputTokens)
		return usage
	}
	newParams := func(forceSubmit bool) anthropic.MessageNewParams {
		p := anthropic.MessageNewParams{
			Model:     suggestModel,
			MaxTokens: 2048,
			System:    []anthropic.TextBlockParam{{Text: system}},
			Messages:  messages,
			Tools:     tools,
		}
		if forceSubmit {
			p.ToolChoice = anthropic.ToolChoiceParamOfTool("submit_suggestions")
		}
		return p
	}

	for i := 0; i <= maxToolIterations; i++ {
		resp, err := a.client.Messages.New(ctx, newParams(i == maxToolIterations))
		if err != nil {
			return SuggestResult{Usage: finalize()}, err
		}
		usage.InputTokens += resp.Usage.InputTokens
		usage.OutputTokens += resp.Usage.OutputTokens
		messages = append(messages, resp.ToParam())

		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range resp.Content {
			tu, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}
			switch tu.Name {
			case "submit_suggestions":
				var in struct {
					Suggestions []Suggestion `json:"suggestions"`
				}
				if err := json.Unmarshal(tu.Input, &in); err != nil {
					return SuggestResult{Usage: finalize()}, fmt.Errorf("ai: parsing submit_suggestions: %w", err)
				}
				return SuggestResult{Suggestions: capSuggestions(in.Suggestions, want), Usage: finalize()}, nil
			case "search_cards":
				var in struct {
					Query string `json:"query"`
				}
				_ = json.Unmarshal(tu.Input, &in)
				var (
					results []CardRef
					serr    error
				)
				if req.Search != nil {
					results, serr = req.Search(ctx, in.Query)
				}
				toolResults = append(toolResults, anthropic.NewToolResultBlock(tu.ID, renderSearchResults(results, serr), serr != nil))
			}
		}

		if len(toolResults) == 0 {
			break
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}

	return SuggestResult{Usage: finalize()}, errNoSuggestions
}

// @spec AI-021
func (a *anthropicAssistant) Explain(ctx context.Context, req ExplainRequest) (ExplainResult, error) {
	system := "You explain why one specific card fits a Magic: The Gathering Commander deck. Answer in " +
		"two or three sentences, grounded in the commander's abilities and the card's own text. Do not " +
		"recommend other cards; do not restate the card's rules verbatim."

	var b strings.Builder
	fmt.Fprintf(&b, "Commander: %s\n", req.CommanderName)
	if req.CommanderText != "" {
		fmt.Fprintf(&b, "Commander text: %s\n", req.CommanderText)
	}
	fmt.Fprintf(&b, "Deck colour identity: %s\n", identityLabel(req.DeckIdentity))
	fmt.Fprintf(&b, "\nCard: %s\n", req.CardName)
	if req.CardText != "" {
		fmt.Fprintf(&b, "Card text: %s\n", req.CardText)
	}
	b.WriteString("\nWhy does this card fit the deck?")

	usage := Usage{Model: explainModel}
	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     explainModel,
		MaxTokens: 512,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(b.String()))},
	})
	if err != nil {
		return ExplainResult{Usage: usage}, err
	}

	usage.InputTokens = resp.Usage.InputTokens
	usage.OutputTokens = resp.Usage.OutputTokens
	usage.CostMicros = estimateCostMicros(usage.Model, usage.InputTokens, usage.OutputTokens)

	var text strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	out := strings.TrimSpace(text.String())
	if out == "" {
		return ExplainResult{Usage: usage}, errors.New("ai: model returned an empty explanation")
	}
	return ExplainResult{Text: out, Usage: usage}, nil
}

// suggestPrompt renders the deck snapshot the suggest loop opens with.
func suggestPrompt(req SuggestRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Commander: %s\n", req.CommanderName)
	if req.CommanderText != "" {
		fmt.Fprintf(&b, "Commander text: %s\n", req.CommanderText)
	}
	fmt.Fprintf(&b, "Deck colour identity: %s\n", identityLabel(req.DeckIdentity))
	if len(req.InDeck) > 0 {
		fmt.Fprintf(&b, "Already in the deck (%d): %s\n", len(req.InDeck), strings.Join(req.InDeck, ", "))
	}
	b.WriteString("\nCandidate cards, most-played first:\n")
	for _, c := range req.Candidates {
		fmt.Fprintf(&b, "- %s", c.Name)
		if c.ManaCost != "" {
			fmt.Fprintf(&b, " %s", c.ManaCost)
		}
		if c.TypeLine != "" {
			fmt.Fprintf(&b, " — %s", c.TypeLine)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderSearchResults turns a search_cards result into the plain text handed
// back to the model as the tool result.
func renderSearchResults(results []CardRef, err error) string {
	if err != nil {
		return "search failed: " + err.Error()
	}
	if len(results) == 0 {
		return "no cards matched."
	}
	var b strings.Builder
	for _, c := range results {
		fmt.Fprintf(&b, "- %s", c.Name)
		if c.ManaCost != "" {
			fmt.Fprintf(&b, " %s", c.ManaCost)
		}
		if c.TypeLine != "" {
			fmt.Fprintf(&b, " — %s", c.TypeLine)
		}
		if c.OracleText != "" {
			fmt.Fprintf(&b, ": %s", oneLine(c.OracleText))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// identityLabel renders a WUBRG identity as "WU" or "colourless".
func identityLabel(id []string) string {
	if len(id) == 0 {
		return "colourless"
	}
	return strings.Join(id, "")
}

// capSuggestions trims the model's list to at most want entries and drops
// blank-named rows.
func capSuggestions(in []Suggestion, want int) []Suggestion {
	out := make([]Suggestion, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s.Name) == "" {
			continue
		}
		out = append(out, s)
		if len(out) == want {
			break
		}
	}
	return out
}
