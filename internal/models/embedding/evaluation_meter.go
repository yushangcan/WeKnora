package embedding

import (
	"context"
	"time"

	evaluationobs "github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/types"
)

type evaluationMeterEmbedder struct {
	inner Embedder
}

func WrapEvaluationMeter(inner Embedder) Embedder {
	if inner == nil {
		return nil
	}
	return &evaluationMeterEmbedder{inner: inner}
}

func (m *evaluationMeterEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if evaluationobs.ObserverFromContext(ctx) == nil {
		return m.inner.Embed(ctx, text)
	}
	startedAt := time.Now()
	result, err := m.inner.Embed(ctx, text)
	m.record(ctx, types.EvaluationOperationEmbed, 1, startedAt, err)
	return result, err
}

func (m *evaluationMeterEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if evaluationobs.ObserverFromContext(ctx) == nil {
		return m.inner.BatchEmbed(ctx, texts)
	}
	startedAt := time.Now()
	result, err := m.inner.BatchEmbed(ctx, texts)
	m.record(ctx, types.EvaluationOperationBatchEmbed, len(texts), startedAt, err)
	return result, err
}

func (m *evaluationMeterEmbedder) BatchEmbedWithPool(
	ctx context.Context,
	_ Embedder,
	texts []string,
) ([][]float32, error) {
	if evaluationobs.ObserverFromContext(ctx) == nil {
		return m.inner.BatchEmbedWithPool(ctx, m.inner, texts)
	}
	startedAt := time.Now()
	// Pass the inner decorator chain into the pool. This records one logical
	// application call and avoids counting its internal sub-batches twice.
	result, err := m.inner.BatchEmbedWithPool(ctx, m.inner, texts)
	m.record(ctx, types.EvaluationOperationBatchEmbed, len(texts), startedAt, err)
	return result, err
}

func (m *evaluationMeterEmbedder) record(
	ctx context.Context,
	operation types.EvaluationModelOperation,
	itemCount int,
	startedAt time.Time,
	callErr error,
) {
	evaluationobs.RecordModelCall(ctx, evaluationobs.ModelCallRecord{
		ModelType:   types.EvaluationModelTypeEmbedding,
		ModelID:     m.inner.GetModelID(),
		ModelName:   m.inner.GetModelName(),
		Operation:   operation,
		DurationMS:  time.Since(startedAt).Milliseconds(),
		CallCount:   1,
		ItemCount:   itemCount,
		UsageSource: types.EvaluationUsageSourceUnavailable,
	}, callErr)
}

func (m *evaluationMeterEmbedder) GetModelName() string { return m.inner.GetModelName() }
func (m *evaluationMeterEmbedder) GetDimensions() int   { return m.inner.GetDimensions() }
func (m *evaluationMeterEmbedder) GetModelID() string   { return m.inner.GetModelID() }
