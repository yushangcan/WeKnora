package rerank

import (
	"context"
	"time"

	evaluationobs "github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/types"
)

type evaluationMeterReranker struct {
	inner Reranker
}

func WrapEvaluationMeter(inner Reranker) Reranker {
	if inner == nil {
		return nil
	}
	return &evaluationMeterReranker{inner: inner}
}

func (m *evaluationMeterReranker) Rerank(
	ctx context.Context,
	query string,
	documents []string,
) ([]RankResult, error) {
	if evaluationobs.ObserverFromContext(ctx) == nil {
		return m.inner.Rerank(ctx, query, documents)
	}
	startedAt := time.Now()
	result, err := m.inner.Rerank(ctx, query, documents)
	evaluationobs.RecordModelCall(ctx, evaluationobs.ModelCallRecord{
		ModelType:   types.EvaluationModelTypeRerank,
		ModelID:     m.inner.GetModelID(),
		ModelName:   m.inner.GetModelName(),
		Operation:   types.EvaluationOperationRerank,
		DurationMS:  time.Since(startedAt).Milliseconds(),
		CallCount:   1,
		ItemCount:   len(documents),
		UsageSource: types.EvaluationUsageSourceUnavailable,
	}, err)
	return result, err
}

func (m *evaluationMeterReranker) GetModelName() string { return m.inner.GetModelName() }
func (m *evaluationMeterReranker) GetModelID() string   { return m.inner.GetModelID() }
