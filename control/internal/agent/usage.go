package agent

import (
	"strings"

	"tethon/internal/protocol"
)

const anthropicPricingSource = "https://platform.claude.com/docs/en/about-claude/pricing"
const anthropicPricingVersion = "2026-06-28"

type modelPricing struct {
	inputPerMTok        float64
	cacheWrite5mPerMTok float64
	cacheWrite1hPerMTok float64
	cacheReadPerMTok    float64
	outputPerMTok       float64
}

func estimateAnthropicCost(model string, tokens protocol.UsageTokens) *protocol.UsageCost {
	pricing, ok := pricingForClaudeModel(model)
	if !ok {
		return &protocol.UsageCost{
			Currency:       "USD",
			Estimated:      true,
			EstimateStatus: "unknown_model",
			PricingSource:  anthropicPricingSource,
			PricingVersion: anthropicPricingVersion,
		}
	}
	cacheWrite5m := tokens.CacheWrite5m
	if cacheWrite5m == 0 && tokens.CacheWrite > 0 {
		cacheWrite5m = tokens.CacheWrite
	}
	inputUSD := cost(tokens.Input, pricing.inputPerMTok)
	outputUSD := cost(tokens.Output, pricing.outputPerMTok)
	cacheReadUSD := cost(tokens.CacheRead, pricing.cacheReadPerMTok)
	cacheWriteUSD := cost(cacheWrite5m, pricing.cacheWrite5mPerMTok) + cost(tokens.CacheWrite1h, pricing.cacheWrite1hPerMTok)
	return &protocol.UsageCost{
		Currency:       "USD",
		Estimated:      true,
		EstimateStatus: "estimated",
		TotalUSD:       inputUSD + outputUSD + cacheReadUSD + cacheWriteUSD,
		InputUSD:       inputUSD,
		OutputUSD:      outputUSD,
		CacheReadUSD:   cacheReadUSD,
		CacheWriteUSD:  cacheWriteUSD,
		PricingSource:  anthropicPricingSource,
		PricingVersion: anthropicPricingVersion,
	}
}

func pricingForClaudeModel(model string) (modelPricing, bool) {
	normalized := strings.ToLower(model)
	switch {
	case strings.Contains(normalized, "fable") || strings.Contains(normalized, "mythos"):
		return modelPricing{inputPerMTok: 10, cacheWrite5mPerMTok: 12.50, cacheWrite1hPerMTok: 20, cacheReadPerMTok: 1, outputPerMTok: 50}, true
	case strings.Contains(normalized, "opus-4-8") || strings.Contains(normalized, "opus-4.8") ||
		strings.Contains(normalized, "opus-4-7") || strings.Contains(normalized, "opus-4.7") ||
		strings.Contains(normalized, "opus-4-6") || strings.Contains(normalized, "opus-4.6") ||
		strings.Contains(normalized, "opus-4-5") || strings.Contains(normalized, "opus-4.5"):
		return modelPricing{inputPerMTok: 5, cacheWrite5mPerMTok: 6.25, cacheWrite1hPerMTok: 10, cacheReadPerMTok: 0.50, outputPerMTok: 25}, true
	case strings.Contains(normalized, "opus-4-1") || strings.Contains(normalized, "opus-4.1") ||
		strings.Contains(normalized, "opus-4-0") || strings.Contains(normalized, "opus-4.0") ||
		strings.Contains(normalized, "opus-4-202505"):
		return modelPricing{inputPerMTok: 15, cacheWrite5mPerMTok: 18.75, cacheWrite1hPerMTok: 30, cacheReadPerMTok: 1.50, outputPerMTok: 75}, true
	case strings.Contains(normalized, "opus"):
		return modelPricing{inputPerMTok: 5, cacheWrite5mPerMTok: 6.25, cacheWrite1hPerMTok: 10, cacheReadPerMTok: 0.50, outputPerMTok: 25}, true
	case strings.Contains(normalized, "sonnet"):
		return modelPricing{inputPerMTok: 3, cacheWrite5mPerMTok: 3.75, cacheWrite1hPerMTok: 6, cacheReadPerMTok: 0.30, outputPerMTok: 15}, true
	case strings.Contains(normalized, "haiku-3-5") || strings.Contains(normalized, "haiku-3.5"):
		return modelPricing{inputPerMTok: 0.80, cacheWrite5mPerMTok: 1, cacheWrite1hPerMTok: 1.60, cacheReadPerMTok: 0.08, outputPerMTok: 4}, true
	case strings.Contains(normalized, "haiku"):
		return modelPricing{inputPerMTok: 1, cacheWrite5mPerMTok: 1.25, cacheWrite1hPerMTok: 2, cacheReadPerMTok: 0.10, outputPerMTok: 5}, true
	default:
		return modelPricing{}, false
	}
}

func cost(tokens int64, perMTok float64) float64 {
	if tokens <= 0 {
		return 0
	}
	return float64(tokens) * perMTok / 1_000_000
}
