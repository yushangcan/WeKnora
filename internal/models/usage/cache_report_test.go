package usage

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func providerCacheEventFixture(t *testing.T, usage types.TokenUsage) types.ModelUsageEvent {
	t.Helper()
	started := time.Unix(100, 0)
	event := buildEvent(
		context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7)),
		ModelMetadata{ModelID: "wiki-chat", ModelName: "wiki-chat", ModelType: types.ModelTypeKnowledgeQA, Provider: "fixture"},
		"chat",
		started,
		1,
		true,
		nil,
		&usage,
	)
	if event == nil {
		t.Fatal("buildEvent returned nil for provider cache fixture")
	}
	return *event
}

func TestWikiProviderCacheReportConsumesProviderUsageFixture(t *testing.T) {
	read := 80
	write := 0
	hitUsage := types.TokenUsage{PromptTokens: 100, CacheReadTokens: read, CacheWriteTokens: write, CacheReported: true, CacheStatus: types.PromptCacheStatusHit}
	missUsage := types.TokenUsage{PromptTokens: 100, CacheMissTokens: 100, CacheReported: true, CacheStatus: types.PromptCacheStatusMiss}

	report := CompareWikiProviderCache(
		WikiProviderCacheCohort{Name: "baseline", PrefixStable: true, Events: []types.ModelUsageEvent{providerCacheEventFixture(t, missUsage)}},
		WikiProviderCacheCohort{Name: "optimized", PrefixStable: true, Events: []types.ModelUsageEvent{providerCacheEventFixture(t, hitUsage)}},
	)

	if report.Baseline.Stats.Evidence != ProviderCacheEvidenceReported || report.Baseline.Stats.CacheHitRate == nil || *report.Baseline.Stats.CacheHitRate != 0 {
		t.Fatalf("baseline fixture report = %#v", report.Baseline.Stats)
	}
	if report.Optimized.Stats.Evidence != ProviderCacheEvidenceReported || report.Optimized.Stats.CacheHitRate == nil || *report.Optimized.Stats.CacheHitRate != 1 {
		t.Fatalf("optimized fixture report = %#v", report.Optimized.Stats)
	}
	if report.Optimized.Stats.CacheReadTokens == nil || *report.Optimized.Stats.CacheReadTokens != int64(read) {
		t.Fatalf("optimized cache read tokens = %#v, want %d", report.Optimized.Stats.CacheReadTokens, read)
	}
}

func TestWikiProviderCacheReportFixturePreservesUnreportedCalls(t *testing.T) {
	reported := types.TokenUsage{PromptTokens: 100, CacheReadTokens: 50, CacheReported: true, CacheStatus: types.PromptCacheStatusHit}
	unreported := types.TokenUsage{PromptTokens: 100}
	report := CompareWikiProviderCache(
		WikiProviderCacheCohort{Name: "mixed", PrefixStable: true, Events: []types.ModelUsageEvent{
			providerCacheEventFixture(t, reported),
			providerCacheEventFixture(t, unreported),
		}},
		WikiProviderCacheCohort{Name: "none", PrefixStable: true, Events: nil},
	)

	if report.Baseline.Stats.Evidence != ProviderCacheEvidenceMixed {
		t.Fatalf("mixed fixture evidence = %q, want mixed", report.Baseline.Stats.Evidence)
	}
	if report.Baseline.Stats.TotalCalls != 2 || report.Baseline.Stats.CacheReportedCalls != 1 {
		t.Fatalf("mixed fixture counts = %#v", report.Baseline.Stats)
	}
	if report.Optimized.Stats.Evidence != ProviderCacheEvidenceUnreported || report.Optimized.Stats.CacheHitRate != nil {
		t.Fatalf("empty fixture invented cache evidence: %#v", report.Optimized.Stats)
	}
}

func TestCompareWikiProviderCacheKeepsProviderEvidenceSeparateFromPrefixStability(t *testing.T) {
	prompt := int64(100)
	read := int64(80)
	missPrompt := int64(100)
	reportedHit := types.ModelUsageEvent{
		CacheReported:   true,
		CacheStatus:     types.ModelUsageCacheStatusHit,
		PromptTokens:    &prompt,
		CacheReadTokens: &read,
	}
	reportedMiss := types.ModelUsageEvent{
		CacheReported: true,
		CacheStatus:   types.ModelUsageCacheStatusMiss,
		PromptTokens:  &missPrompt,
	}

	report := CompareWikiProviderCache(
		WikiProviderCacheCohort{
			Name:              "baseline",
			PrefixFingerprint: "prefix-baseline",
			PrefixStable:      true,
			Events:            []types.ModelUsageEvent{reportedMiss},
		},
		WikiProviderCacheCohort{
			Name:              "optimized",
			PrefixFingerprint: "prefix-optimized",
			PrefixStable:      true,
			Events:            []types.ModelUsageEvent{reportedHit},
		},
	)

	if !report.Baseline.PrefixStable || !report.Optimized.PrefixStable {
		t.Fatal("prefix stability evidence was lost")
	}
	if report.Baseline.Stats.CacheHitRate == nil || *report.Baseline.Stats.CacheHitRate != 0 {
		t.Fatalf("baseline cache hit rate = %#v, want reported zero", report.Baseline.Stats.CacheHitRate)
	}
	if report.Optimized.Stats.CacheHitRate == nil || *report.Optimized.Stats.CacheHitRate != 1 {
		t.Fatalf("optimized cache hit rate = %#v, want one", report.Optimized.Stats.CacheHitRate)
	}
	if report.Optimized.Stats.CachedTokenRatio == nil || *report.Optimized.Stats.CachedTokenRatio != 0.8 {
		t.Fatalf("optimized cached token ratio = %#v, want 0.8", report.Optimized.Stats.CachedTokenRatio)
	}
}

func TestCompareWikiProviderCacheDoesNotInventUnreportedValues(t *testing.T) {
	prompt := int64(100)
	reported := types.ModelUsageEvent{PromptTokens: &prompt}
	report := CompareWikiProviderCache(
		WikiProviderCacheCohort{Name: "baseline", PrefixStable: true, Events: []types.ModelUsageEvent{{}}},
		WikiProviderCacheCohort{Name: "optimized", PrefixStable: true, Events: []types.ModelUsageEvent{reported}},
	)

	if report.Baseline.Stats.Evidence != "unreported" || report.Baseline.Stats.CacheHitRate != nil {
		t.Fatalf("unreported baseline should keep nil cache evidence: %#v", report.Baseline.Stats)
	}
	if report.Optimized.Stats.Evidence != "unreported" || report.Optimized.Stats.CacheHitRate != nil {
		t.Fatalf("cache fields absent should not become a miss or zero rate: %#v", report.Optimized.Stats)
	}
	if report.Optimized.Stats.PromptTokens == nil || *report.Optimized.Stats.PromptTokens != 100 {
		t.Fatalf("explicit prompt token usage was lost: %#v", report.Optimized.Stats.PromptTokens)
	}
}

func TestCompareWikiProviderCacheMarksMixedEvidence(t *testing.T) {
	reported := types.ModelUsageEvent{CacheReported: true, CacheStatus: types.ModelUsageCacheStatusHit}
	unreported := types.ModelUsageEvent{}
	report := CompareWikiProviderCache(
		WikiProviderCacheCohort{Events: []types.ModelUsageEvent{reported, unreported}},
		WikiProviderCacheCohort{},
	)
	if report.Baseline.Stats.Evidence != "mixed" {
		t.Fatalf("mixed cache evidence = %q, want mixed", report.Baseline.Stats.Evidence)
	}
	if report.Baseline.Stats.CacheHitRate == nil || *report.Baseline.Stats.CacheHitRate != 1 {
		t.Fatalf("reported subset should still have a rate: %#v", report.Baseline.Stats.CacheHitRate)
	}
}
