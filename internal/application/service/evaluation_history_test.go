package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type evaluationHistoryRepositoryStub struct {
	interfaces.EvaluationRepository
	overviews []types.EvaluationRunOverview
	tenantID  uint64
}

func (s *evaluationHistoryRepositoryStub) GetRunOverviews(
	_ context.Context,
	tenantID uint64,
	_ []string,
) ([]types.EvaluationRunOverview, error) {
	s.tenantID = tenantID
	return s.overviews, nil
}

func comparisonOverview(runID string, precision, rougeL, amount float64) types.EvaluationRunOverview {
	config := &types.EvaluationRunConfig{
		SchemaVersion: types.EvaluationConfigSchemaVersion,
		Dataset: types.EvaluationDatasetDescriptor{
			ID: "default", ContentFingerprint: "sha256:dataset",
		},
		Runtime: types.EvaluationRuntimeConfig{
			MetricVersion: types.EvaluationMetricVersion,
			ResultVersion: types.EvaluationResultSchemaVersion,
		},
		ConfigHash: "sha256:" + runID,
	}
	return types.EvaluationRunOverview{
		Config: config,
		Summary: types.EvaluationRunSummary{
			RunID:               runID,
			Status:              types.EvaluationRunStatusSuccess,
			Dataset:             config.Dataset,
			ConfigHash:          config.ConfigHash,
			MetricVersion:       config.Runtime.MetricVersion,
			ResultVersion:       config.Runtime.ResultVersion,
			Retrieval:           &types.EvaluationRetrievalResult{Precision: precision},
			Answer:              &types.EvaluationAnswerResult{ROUGEL: rougeL},
			Cost:                types.EvaluationCostResult{Status: types.EvaluationCostStatusComplete, Amount: &amount, Currency: "USD", PricingVersion: "v1"},
			Timing:              types.EvaluationTimingResult{TotalWallTimeMS: int64(precision * 1000)},
			Usage:               types.EvaluationUsageResult{Status: types.EvaluationUsageStatusComplete, Calls: types.EvaluationCallCounts{Total: 2}, Tokens: types.EvaluationTokenTotals{TotalTokens: 100}},
			Warnings:            []types.EvaluationWarning{},
			Reproducibility:     types.EvaluationReproducibility{Status: types.EvaluationReproducibilityComplete},
			ConfigSchemaVersion: types.EvaluationConfigSchemaVersion,
		},
	}
}

func TestCompareEvaluationOverviewUsesCandidateMinusBaseline(t *testing.T) {
	baseline := comparisonOverview("baseline", 0.5, 0.4, 1.0)
	candidate := comparisonOverview("candidate", 0.6, 0.5, 0.8)
	result := compareEvaluationOverview(baseline, candidate)

	if !result.QualityCompatibility.Comparable || !result.CostCompatibility.Comparable ||
		!result.UsageCompatibility.Comparable ||
		!result.TimingCompatibility.Comparable {
		t.Fatalf("compatible runs were rejected: %#v", result)
	}
	if result.Quality.Precision.Absolute == nil || math.Abs(*result.Quality.Precision.Absolute-0.1) > 1e-9 {
		t.Fatalf("precision delta = %#v", result.Quality.Precision)
	}
	if result.Cost.Amount.Absolute == nil || math.Abs(*result.Cost.Amount.Absolute+0.2) > 1e-9 {
		t.Fatalf("cost delta = %#v", result.Cost.Amount)
	}
	if result.Usage.TotalTokens.Absolute == nil || *result.Usage.TotalTokens.Absolute != 0 {
		t.Fatalf("usage delta = %#v", result.Usage.TotalTokens)
	}
	if result.Timing.TotalWallTimeMS.Absolute == nil || *result.Timing.TotalWallTimeMS.Absolute != 100 {
		t.Fatalf("timing delta = %#v", result.Timing.TotalWallTimeMS)
	}
}

func TestEvaluationComparisonRejectsIncompatibleQualityAndUnknownCost(t *testing.T) {
	baseline := comparisonOverview("baseline", 0.5, 0.4, 1.0)
	candidate := comparisonOverview("candidate", 0.6, 0.5, 0.8)
	candidate.Summary.Dataset.ContentFingerprint = "sha256:other"
	candidate.Summary.MetricVersion = "retrieval-generation/v1"
	candidate.Summary.Cost.Amount = nil
	candidate.Summary.Cost.Status = types.EvaluationCostStatusUnavailable

	result := compareEvaluationOverview(baseline, candidate)
	if result.QualityCompatibility.Comparable || len(result.QualityCompatibility.Reasons) != 2 {
		t.Fatalf("quality mismatch was not explained: %#v", result.QualityCompatibility)
	}
	if result.CostCompatibility.Comparable || result.Cost.Amount.Absolute != nil {
		t.Fatalf("unknown cost was compared: compatibility=%#v delta=%#v", result.CostCompatibility, result.Cost.Amount)
	}
	if !result.UsageCompatibility.Comparable || result.Usage.TotalTokens.Absolute == nil {
		t.Fatalf("usage was coupled to unavailable cost: compatibility=%#v delta=%#v", result.UsageCompatibility, result.Usage.TotalTokens)
	}
	if !result.TimingCompatibility.Comparable {
		t.Fatalf("observed timing should remain comparable: %#v", result.TimingCompatibility)
	}
}

func TestEvaluationComparisonRejectsUnavailableUsageIndependently(t *testing.T) {
	baseline := comparisonOverview("baseline", 0.5, 0.4, 1.0)
	candidate := comparisonOverview("candidate", 0.6, 0.5, 0.8)
	candidate.Summary.Usage.Status = types.EvaluationUsageStatusUnavailable

	result := compareEvaluationOverview(baseline, candidate)
	if result.UsageCompatibility.Comparable || result.Usage.TotalTokens.Absolute != nil {
		t.Fatalf("unavailable usage was compared: compatibility=%#v delta=%#v", result.UsageCompatibility, result.Usage.TotalTokens)
	}
	if !result.CostCompatibility.Comparable || !result.TimingCompatibility.Comparable {
		t.Fatalf("usage availability affected another dimension: %#v", result)
	}
}

func TestEvaluationNumberDeltaAvoidsDivisionByZero(t *testing.T) {
	delta := evaluationNumberDelta(0, 2)
	if delta.Absolute == nil || *delta.Absolute != 2 || delta.Percent != nil {
		t.Fatalf("zero baseline delta = %#v", delta)
	}
}

func TestCompareEvaluationRunsPreservesRequestedOrderAndTenant(t *testing.T) {
	runA := comparisonOverview("run-a", 0.5, 0.4, 1)
	runB := comparisonOverview("run-b", 0.6, 0.5, 2)
	repo := &evaluationHistoryRepositoryStub{overviews: []types.EvaluationRunOverview{runA, runB}}
	svc := &EvaluationService{evaluationRepository: repo}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	comparison, err := svc.CompareEvaluationRuns(ctx, "run-a", []string{"run-b", "run-a", "run-b"})
	if err != nil {
		t.Fatalf("compare runs: %v", err)
	}
	if repo.tenantID != 7 || len(comparison.Runs) != 2 ||
		comparison.Runs[0].Run.RunID != "run-b" || comparison.Runs[1].Run.RunID != "run-a" {
		t.Fatalf("comparison order or tenant changed: %#v", comparison)
	}

	repo.overviews = []types.EvaluationRunOverview{runA}
	if _, err := svc.CompareEvaluationRuns(ctx, "run-a", []string{"run-a", "run-b"}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing tenant-scoped run returned %v", err)
	}
}

func TestCompareEvaluationRunsValidatesSelection(t *testing.T) {
	svc := &EvaluationService{evaluationRepository: &evaluationHistoryRepositoryStub{}}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	if _, err := svc.CompareEvaluationRuns(ctx, "run-a", []string{"run-a"}); err == nil {
		t.Fatal("single-run comparison was accepted")
	}
	if _, err := svc.CompareEvaluationRuns(ctx, "missing", []string{"run-a", "run-b"}); err == nil {
		t.Fatal("baseline outside selection was accepted")
	}
}
