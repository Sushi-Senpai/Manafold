package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

func ip(n int) *int         { return &n }
func fp(f float64) *float64 { return &f }

func sampleCurateRequest() CurateRequest {
	return CurateRequest{
		Step:             "picks",
		StepGuidance:     "Choose cards that support the commander.",
		CommanderName:    "Krenko, Mob Boss",
		CommanderText:    "{T}: Create X 1/1 red Goblin creature tokens, where X is Krenko's power.",
		DeckIdentity:     []string{"R"},
		Bracket:          3,
		BracketGuidance:  "Bracket 3 (Upgraded): up to three Game Changers.",
		GameChangersLeft: ip(2),
		Notes:            "goblins and tokens",
		InDeck:           []string{"Sol Ring", "Mountain"},
		Pool: []PoolCard{
			{
				CardRef:      CardRef{Name: "Goblin Chieftain", ManaCost: "{1}{R}{R}", TypeLine: "Creature — Goblin", OracleText: "Haste\nOther Goblins you control have haste."},
				PriceUSD:     fp(1.5),
				InclusionPct: ip(85),
				SynergyPct:   ip(62),
			},
			{
				CardRef:     CardRef{Name: "Jeska's Will", ManaCost: "{2}{R}", TypeLine: "Instant"},
				GameChanger: true,
			},
			{
				CardRef:      CardRef{Name: "Cavern of Souls", TypeLine: "Land"},
				ProducedMana: []string{"R", "G"},
			},
		},
		Want: 5,
	}
}

// @spec AI-052
func TestCuratePrompt_RendersPoolRulesAndLabelsNotesUntrusted(t *testing.T) {
	p := curatePrompt(sampleCurateRequest())
	for _, want := range []string{
		"Krenko, Mob Boss",
		"Deck colour identity: R",
		"Bracket 3 (Upgraded): up to three Game Changers.",
		"2 more Game Changers",
		"Choose cards that support the commander.",
		"goblins and tokens",
		"untrusted",
		"Sol Ring, Mountain",
		"- Goblin Chieftain {1}{R}{R} — Creature — Goblin",
		"$1.50",
		"in 85% of sampled decks",
		"+62 synergy",
		"Haste Other Goblins you control have haste.",
		"- Jeska's Will {2}{R} — Instant [Game Changer]",
		"makes R, G",
		"Pick up to 5",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
}

// @spec AI-052
func TestCuratePrompt_GameChangerBudgetWording(t *testing.T) {
	r := sampleCurateRequest()
	r.GameChangersLeft = ip(0)
	if p := curatePrompt(r); !strings.Contains(p, "No Game Changers") {
		t.Errorf("zero budget not stated:\n%s", p)
	}
	r.GameChangersLeft = nil
	if p := curatePrompt(r); !strings.Contains(p, "no Game Changer limit") {
		t.Errorf("unlimited budget not stated:\n%s", p)
	}
}

// @spec AI-052
func TestCuratePrompt_OmitsEmptyNotes(t *testing.T) {
	r := sampleCurateRequest()
	r.Notes = "  \n "
	if p := curatePrompt(r); strings.Contains(p, "untrusted") {
		t.Errorf("empty notes still rendered a notes block:\n%s", p)
	}
}

// @spec AI-052
func TestCuratePrompt_TruncatesLongOracleText(t *testing.T) {
	r := sampleCurateRequest()
	r.Pool = []PoolCard{{CardRef: CardRef{Name: "Wall of Words", OracleText: strings.Repeat("word ", 200)}}}
	p := curatePrompt(r)
	if strings.Count(p, "word") > 70 || !strings.Contains(p, "…") {
		t.Errorf("oracle text not truncated:\n%s", p)
	}
}

// fakeAnthropic answers every /v1/messages call with the given JSON body and
// records the request body.
func fakeAnthropic(t *testing.T, status int, body string, got *map[string]any) *anthropicAssistant {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if got != nil {
			_ = json.Unmarshal(raw, got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return newAnthropicAssistant("sk-ant-test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
}

func messageWithTool(input string, inTok, outTok int) string {
	return `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5",
"content":[{"type":"tool_use","id":"toolu_1","name":"submit_picks","input":` + input + `}],
"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":` + itoa(inTok) + `,"output_tokens":` + itoa(outTok) + `}}`
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// @spec AI-050, AI-052, AI-053
func TestCurate_ForcesSubmitPicksAndCapsPicks(t *testing.T) {
	var sent map[string]any
	a := fakeAnthropic(t, 200, messageWithTool(
		`{"summary":" Goblins first. ","picks":[{"name":"A","reason":"r1"},{"name":"  ","reason":"blank"},{"name":"B","reason":"r2"},{"name":"C","reason":"r3"}]}`,
		1200, 300), &sent)

	req := sampleCurateRequest()
	req.Want = 2
	res, err := a.Curate(context.Background(), req)
	if err != nil {
		t.Fatalf("Curate: %v", err)
	}
	if res.Summary != "Goblins first." {
		t.Errorf("summary = %q", res.Summary)
	}
	if len(res.Picks) != 2 || res.Picks[0].Name != "A" || res.Picks[1].Name != "B" {
		t.Errorf("picks = %+v, want [A B] (blank dropped, capped at Want)", res.Picks)
	}
	if res.Usage.Model != curateModel || res.Usage.InputTokens != 1200 || res.Usage.OutputTokens != 300 || res.Usage.CostMicros == 0 {
		t.Errorf("usage = %+v", res.Usage)
	}

	if sent["model"] != curateModel {
		t.Errorf("model sent = %v", sent["model"])
	}
	choice, _ := sent["tool_choice"].(map[string]any)
	if choice["type"] != "tool" || choice["name"] != "submit_picks" {
		t.Errorf("tool_choice = %v, want the forced submit_picks tool", sent["tool_choice"])
	}
	tools, _ := sent["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v, want exactly submit_picks (no search tool)", sent["tools"])
	}
	if name := tools[0].(map[string]any)["name"]; name != "submit_picks" {
		t.Errorf("tool = %v", name)
	}
}

// An answer that picks nothing is a valid, empty result — not an error.
func TestCurate_EmptyPicksIsNotAnError(t *testing.T) {
	a := fakeAnthropic(t, 200, messageWithTool(`{"summary":"Nothing fits.","picks":[]}`, 500, 20), nil)
	res, err := a.Curate(context.Background(), sampleCurateRequest())
	if err != nil {
		t.Fatalf("Curate: %v", err)
	}
	if len(res.Picks) != 0 || res.Summary != "Nothing fits." {
		t.Errorf("res = %+v", res)
	}
}

// @spec AI-052, AI-053
func TestCurate_NoToolCallIsAnErrorThatStillReportsUsage(t *testing.T) {
	a := fakeAnthropic(t, 200, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5",
"content":[{"type":"text","text":"I would pick Goblin Chieftain."}],
"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":900,"output_tokens":15}}`, nil)
	res, err := a.Curate(context.Background(), sampleCurateRequest())
	if err == nil {
		t.Fatal("Curate with no submit_picks call returned no error")
	}
	if res.Usage.InputTokens != 900 || res.Usage.OutputTokens != 15 {
		t.Errorf("usage on the error path = %+v, want the call's tokens", res.Usage)
	}
}

// @spec AI-053
func TestCurate_TruncatedPayloadIsAnErrorThatStillReportsUsage(t *testing.T) {
	a := fakeAnthropic(t, 200, messageWithTool(`{"summary":"cut off","picks":"not-an-array"}`, 700, 2048), nil)
	res, err := a.Curate(context.Background(), sampleCurateRequest())
	if err == nil {
		t.Fatal("unparseable submit_picks input returned no error")
	}
	if res.Usage.InputTokens != 700 || res.Usage.OutputTokens != 2048 {
		t.Errorf("usage = %+v", res.Usage)
	}
}

// A transport-level failure never reached the model: zero usage, so the handler
// will not meter it (AI-034).
func TestCurate_ProviderErrorHasZeroUsage(t *testing.T) {
	a := fakeAnthropic(t, 500, `{"type":"error","error":{"type":"api_error","message":"boom"}}`, nil)
	res, err := a.Curate(context.Background(), sampleCurateRequest())
	if err == nil {
		t.Fatal("expected an error")
	}
	if res.Usage.InputTokens != 0 || res.Usage.OutputTokens != 0 {
		t.Errorf("usage = %+v, want zero", res.Usage)
	}
}
