package usage

import "github.com/Tencent/WeKnora/internal/types"

// ProviderCacheEvidence describes how much of a cohort has explicit cache
// accounting from the Provider.
type ProviderCacheEvidence string

const (
	ProviderCacheEvidenceReported   ProviderCacheEvidence = "reported"
	ProviderCacheEvidenceUnreported ProviderCacheEvidence = "unreported"
	ProviderCacheEvidenceMixed      ProviderCacheEvidence = "mixed"
)

// WikiProviderCacheCohort is the redacted input to a Wiki provider-cache
// comparison. The caller supplies only the irreversible prefix fingerprint
// and usage events; prompt text and document contents never enter the report.
type WikiProviderCacheCohort struct {
	Name              string                  `json:"name"`
	PrefixFingerprint string                  `json:"prefix_fingerprint,omitempty"`
	PrefixStable      bool                    `json:"prefix_stable"`
	Events            []types.ModelUsageEvent `json:"-"`
}

// WikiProviderCacheStats contains evidence that can be calculated from
// provider-reported usage. Nullable totals preserve the difference between a
// provider reporting zero and a provider omitting the field entirely.
// PromptTokens sums all explicit prompt usage. CachedTokenRatio uses only
// cache-reported calls with both prompt and cache-read token counts.
type WikiProviderCacheStats struct {
	TotalCalls         int64                 `json:"total_calls"`
	CacheReportedCalls int64                 `json:"cache_reported_calls"`
	CacheHitCalls      int64                 `json:"cache_hit_calls"`
	CacheMissCalls     int64                 `json:"cache_miss_calls"`
	PromptTokens       *int64                `json:"prompt_tokens,omitempty"`
	CacheReadTokens    *int64                `json:"cache_read_tokens,omitempty"`
	CacheHitRate       *float64              `json:"cache_hit_rate,omitempty"`
	CachedTokenRatio   *float64              `json:"cached_token_ratio,omitempty"`
	Evidence           ProviderCacheEvidence `json:"evidence"`
}

// WikiProviderCacheComparison is deliberately a report value rather than a
// persisted model. It is suitable for a test artifact or an operator report;
// a Provider that does not return cache fields yields unreported evidence and
// nil rates instead of a fabricated zero-percent result.
type WikiProviderCacheComparison struct {
	Baseline  WikiProviderCacheCohortReport `json:"baseline"`
	Optimized WikiProviderCacheCohortReport `json:"optimized"`
}

// WikiProviderCacheCohortReport combines redacted prefix evidence with usage
// counters for one before/after cohort.
type WikiProviderCacheCohortReport struct {
	Name              string                 `json:"name"`
	PrefixFingerprint string                 `json:"prefix_fingerprint,omitempty"`
	PrefixStable      bool                   `json:"prefix_stable"`
	Stats             WikiProviderCacheStats `json:"stats"`
}

// CompareWikiProviderCache builds a before/after report without making any
// Provider calls. Cache rates are defined only over calls that explicitly
// report cache accounting, matching the model-usage aggregation contract.
func CompareWikiProviderCache(baseline, optimized WikiProviderCacheCohort) WikiProviderCacheComparison {
	return WikiProviderCacheComparison{
		Baseline:  buildWikiProviderCacheCohortReport(baseline),
		Optimized: buildWikiProviderCacheCohortReport(optimized),
	}
}

func buildWikiProviderCacheCohortReport(cohort WikiProviderCacheCohort) WikiProviderCacheCohortReport {
	return WikiProviderCacheCohortReport{
		Name:              cohort.Name,
		PrefixFingerprint: cohort.PrefixFingerprint,
		PrefixStable:      cohort.PrefixStable,
		Stats:             summarizeWikiProviderCache(cohort.Events),
	}
}

func summarizeWikiProviderCache(events []types.ModelUsageEvent) WikiProviderCacheStats {
	stats := WikiProviderCacheStats{TotalCalls: int64(len(events))}
	var promptTokens, cacheReadTokens int64
	var pairedPromptTokens, pairedCacheReadTokens int64
	var promptReported, cacheReadReported bool
	for _, event := range events {
		// Token usage remains useful even when the provider omits cache fields.
		if event.PromptTokens != nil {
			promptTokens += *event.PromptTokens
			promptReported = true
		}
		if !event.CacheReported {
			continue
		}
		stats.CacheReportedCalls++
		switch event.CacheStatus {
		case types.ModelUsageCacheStatusHit:
			stats.CacheHitCalls++
		case types.ModelUsageCacheStatusMiss:
			stats.CacheMissCalls++
		}
		if event.CacheReadTokens != nil {
			cacheReadTokens += *event.CacheReadTokens
			cacheReadReported = true
		}
		// Numerator and denominator must describe the same observed calls.
		if event.PromptTokens != nil && event.CacheReadTokens != nil {
			pairedPromptTokens += *event.PromptTokens
			pairedCacheReadTokens += *event.CacheReadTokens
		}
	}

	switch {
	case stats.CacheReportedCalls == 0:
		stats.Evidence = ProviderCacheEvidenceUnreported
	case stats.CacheReportedCalls == int64(len(events)):
		stats.Evidence = ProviderCacheEvidenceReported
	default:
		stats.Evidence = ProviderCacheEvidenceMixed
	}
	if stats.CacheReportedCalls > 0 {
		hitRate := float64(stats.CacheHitCalls) / float64(stats.CacheReportedCalls)
		stats.CacheHitRate = &hitRate
	}
	if promptReported {
		stats.PromptTokens = &promptTokens
	}
	if cacheReadReported {
		stats.CacheReadTokens = &cacheReadTokens
	}
	if pairedPromptTokens > 0 {
		ratio := float64(pairedCacheReadTokens) / float64(pairedPromptTokens)
		stats.CachedTokenRatio = &ratio
	}
	return stats
}
