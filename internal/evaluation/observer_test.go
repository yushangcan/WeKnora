package evaluation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestObserverSnapshotAggregatesExistingMetricsUsageAndTiming(t *testing.T) {
	startedAt := time.Now().Add(-time.Second)
	observer := NewObserver("run-1", 9, "default", startedAt)
	ctx := WithEvaluationRun(context.Background(), observer)

	caseCtx, finishCase := observer.StartCase(ctx, "42")
	RecordModelCall(caseCtx, ModelCallRecord{
		ModelType:   types.EvaluationModelTypeChat,
		ModelID:     "chat-1",
		ModelName:   "fake-chat",
		Operation:   types.EvaluationOperationChat,
		DurationMS:  25,
		CallCount:   1,
		ItemCount:   1,
		UsageSource: types.EvaluationUsageSourceProviderReported,
		Usage: &types.TokenUsage{
			PromptTokens:     5,
			CompletionTokens: 3,
			TotalTokens:      8,
		},
	}, nil)
	RecordModelCall(caseCtx, ModelCallRecord{
		ModelType:   types.EvaluationModelTypeRerank,
		ModelID:     "rerank-1",
		ModelName:   "fake-rerank",
		Operation:   types.EvaluationOperationRerank,
		DurationMS:  10,
		CallCount:   1,
		ItemCount:   4,
		UsageSource: types.EvaluationUsageSourceUnavailable,
	}, nil)
	finishCase(nil)
	observer.Complete()

	metric := &types.MetricResult{
		RetrievalMetrics:  types.RetrievalMetrics{Precision: 0.75, Recall: 0.5},
		GenerationMetrics: types.GenerationMetrics{BLEU1: 0.25, ROUGEL: 0.4},
	}
	result := observer.Snapshot(types.EvaluationStatueSuccess, metric)
	if result.Run.Status != types.EvaluationRunStatusSuccess {
		t.Fatalf("run status = %q", result.Run.Status)
	}
	if result.Retrieval == nil || result.Retrieval.Precision != metric.RetrievalMetrics.Precision {
		t.Fatalf("existing retrieval metric was not mapped: %#v", result.Retrieval)
	}
	if result.Answer == nil || result.Answer.ROUGEL != metric.GenerationMetrics.ROUGEL {
		t.Fatalf("existing answer metric was not mapped: %#v", result.Answer)
	}
	if result.Usage.Status != types.EvaluationUsageStatusPartial || result.Usage.Calls.Total != 2 {
		t.Fatalf("unexpected usage aggregate: %#v", result.Usage)
	}
	if result.Usage.Tokens.TotalTokens != 8 || result.Timing.ModelCallCumulativeMS != 35 {
		t.Fatalf("usage or model timing was not aggregated: usage=%#v timing=%#v", result.Usage, result.Timing)
	}
	if result.Cost.Status != types.EvaluationCostStatusUnavailable || result.Cost.Amount != nil {
		t.Fatalf("unknown cost was not represented as null/unavailable: %#v", result.Cost)
	}
	if len(result.Cases) != 1 || result.Cases[0].CaseID != "42" {
		t.Fatalf("unexpected case snapshot: %#v", result.Cases)
	}
	if result.Cases[0].Warnings == nil {
		t.Fatal("case warnings must serialize as an empty array, not null")
	}
}

func TestFailedObserverWithPartialDataUsesPartialStatus(t *testing.T) {
	observer := NewObserver("run-failed", 1, "default", time.Now())
	ctx := WithEvaluationRun(context.Background(), observer)
	_, finishCase := observer.StartCase(ctx, "case-1")
	finishCase(errors.New("provider unavailable"))
	observer.Complete()

	result := observer.Snapshot(types.EvaluationStatueFailed, nil)
	if result.Run.Status != types.EvaluationRunStatusPartial {
		t.Fatalf("status = %q, want partial", result.Run.Status)
	}
	if len(result.Cases) != 1 || result.Cases[0].Status != types.EvaluationRunStatusFailed {
		t.Fatalf("failed case was not retained: %#v", result.Cases)
	}
}
