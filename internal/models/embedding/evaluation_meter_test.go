package embedding

import (
	"context"
	"errors"
	"testing"
	"time"

	evaluationobs "github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/types"
)

type evaluationMeterFakeEmbedder struct {
	err error
}

func (f *evaluationMeterFakeEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 2}, f.err
}

func (f *evaluationMeterFakeEmbedder) BatchEmbed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range result {
		result[i] = []float32{float32(i)}
	}
	return result, f.err
}

func (f *evaluationMeterFakeEmbedder) BatchEmbedWithPool(
	ctx context.Context,
	model Embedder,
	texts []string,
) ([][]float32, error) {
	return model.BatchEmbed(ctx, texts)
}

func (f *evaluationMeterFakeEmbedder) GetModelName() string { return "fake-embedding" }
func (f *evaluationMeterFakeEmbedder) GetDimensions() int   { return 2 }
func (f *evaluationMeterFakeEmbedder) GetModelID() string   { return "embedding-1" }

func TestEvaluationEmbeddingMeterRecordsLogicalCallsWithoutPoolDuplicates(t *testing.T) {
	inner := &evaluationMeterFakeEmbedder{}
	metered := WrapEvaluationMeter(inner)
	observer := evaluationobs.NewObserver("run-1", 1, "default", time.Now())
	ctx := evaluationobs.WithEvaluationRun(context.Background(), observer)

	vector, err := metered.Embed(ctx, "text")
	if err != nil || len(vector) != 2 {
		t.Fatalf("Embed result changed: vector=%v err=%v", vector, err)
	}
	batch, err := metered.BatchEmbed(ctx, []string{"a", "b"})
	if err != nil || len(batch) != 2 {
		t.Fatalf("BatchEmbed result changed: batch=%v err=%v", batch, err)
	}
	pooled, err := metered.BatchEmbedWithPool(ctx, metered, []string{"a", "b", "c"})
	if err != nil || len(pooled) != 3 {
		t.Fatalf("BatchEmbedWithPool result changed: pooled=%v err=%v", pooled, err)
	}

	records := observer.CollectorSnapshot()
	if len(records) != 3 {
		t.Fatalf("record count = %d, want 3: %#v", len(records), records)
	}
	if records[2].ItemCount != 3 || records[2].UsageSource != types.EvaluationUsageSourceUnavailable {
		t.Fatalf("unexpected pooled record: %#v", records[2])
	}

	if _, err := metered.Embed(context.Background(), "plain request"); err != nil {
		t.Fatalf("plain-context Embed result changed: %v", err)
	}
	if got := len(observer.CollectorSnapshot()); got != 3 {
		t.Fatalf("plain context added an evaluation record: %d", got)
	}
}

func TestEvaluationEmbeddingMeterPreservesError(t *testing.T) {
	expectedErr := errors.New("embedding failed")
	inner := &evaluationMeterFakeEmbedder{err: expectedErr}
	metered := WrapEvaluationMeter(inner)
	observer := evaluationobs.NewObserver("run-1", 1, "default", time.Now())
	ctx := evaluationobs.WithEvaluationRun(context.Background(), observer)

	_, err := metered.BatchEmbed(ctx, []string{"a"})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("wrapper changed error: %v", err)
	}
	if records := observer.CollectorSnapshot(); len(records) != 1 || records[0].Success {
		t.Fatalf("failed call was not recorded: %#v", records)
	}
}
