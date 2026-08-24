package rerank

import (
	"context"
	"errors"
	"testing"
	"time"

	evaluationobs "github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/types"
)

type evaluationMeterFakeReranker struct {
	results []RankResult
	err     error
}

func (f *evaluationMeterFakeReranker) Rerank(
	context.Context,
	string,
	[]string,
) ([]RankResult, error) {
	return f.results, f.err
}

func (f *evaluationMeterFakeReranker) GetModelName() string { return "fake-rerank" }
func (f *evaluationMeterFakeReranker) GetModelID() string   { return "rerank-1" }

func TestEvaluationRerankMeterPreservesResultsAndRecordsDocuments(t *testing.T) {
	expected := []RankResult{{Index: 1, RelevanceScore: 0.9}}
	inner := &evaluationMeterFakeReranker{results: expected}
	metered := WrapEvaluationMeter(inner)
	observer := evaluationobs.NewObserver("run-1", 1, "default", time.Now())
	ctx := evaluationobs.WithEvaluationRun(context.Background(), observer)

	results, err := metered.Rerank(ctx, "query", []string{"a", "b"})
	if err != nil || len(results) != 1 || results[0] != expected[0] {
		t.Fatalf("wrapper changed results: results=%#v err=%v", results, err)
	}
	records := observer.CollectorSnapshot()
	if len(records) != 1 || records[0].ItemCount != 2 || records[0].UsageSource != types.EvaluationUsageSourceUnavailable {
		t.Fatalf("unexpected records: %#v", records)
	}

	if _, err := metered.Rerank(context.Background(), "plain query", []string{"a"}); err != nil {
		t.Fatalf("plain-context Rerank result changed: %v", err)
	}
	if got := len(observer.CollectorSnapshot()); got != 1 {
		t.Fatalf("plain context added an evaluation record: %d", got)
	}
}

func TestEvaluationRerankMeterPreservesError(t *testing.T) {
	expectedErr := errors.New("rerank failed")
	inner := &evaluationMeterFakeReranker{err: expectedErr}
	metered := WrapEvaluationMeter(inner)
	observer := evaluationobs.NewObserver("run-1", 1, "default", time.Now())
	ctx := evaluationobs.WithEvaluationRun(context.Background(), observer)

	_, err := metered.Rerank(ctx, "query", []string{"a"})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("wrapper changed error: %v", err)
	}
	if records := observer.CollectorSnapshot(); len(records) != 1 || records[0].Success {
		t.Fatalf("failed call was not recorded: %#v", records)
	}
}
