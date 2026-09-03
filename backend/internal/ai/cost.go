package ai

// modelPrice is per-million-token USD pricing, in micro-USD (millionths of a
// dollar) so the running ai_usage.cost_micros sum stays integer. Values are the
// first-party Claude API rates; a model not listed falls back to Sonnet-tier,
// which never under-charges relative to Haiku.
type modelPrice struct {
	inputMicrosPerMTok  int64
	outputMicrosPerMTok int64
}

var priceTable = map[string]modelPrice{
	// $2 / $10 per MTok.
	"claude-sonnet-5": {inputMicrosPerMTok: 2_000_000, outputMicrosPerMTok: 10_000_000},
	// $1 / $5 per MTok.
	"claude-haiku-4-5": {inputMicrosPerMTok: 1_000_000, outputMicrosPerMTok: 5_000_000},
}

// estimateCostMicros returns the estimated cost of a call in micro-USD from its
// token counts. An unknown model is priced at the Sonnet rate.
func estimateCostMicros(model string, inputTokens, outputTokens int64) int64 {
	p, ok := priceTable[model]
	if !ok {
		p = priceTable["claude-sonnet-5"]
	}
	in := inputTokens * p.inputMicrosPerMTok / 1_000_000
	out := outputTokens * p.outputMicrosPerMTok / 1_000_000
	return in + out
}
