package evaluation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestModelCallCollectorConcurrentSnapshot(t *testing.T) {
	observer := NewObserver("run-1", 1, "default", time.Now())
	ctx := WithEvaluationRun(context.Background(), observer)

	const callCount = 128
	var wg sync.WaitGroup
	for i := 0; i < callCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			caseCtx := WithEvaluationCase(WithEvaluationPhase(ctx, types.EvaluationPhaseEvaluation), "case")
			RecordModelCall(caseCtx, ModelCallRecord{
				ModelType:   types.EvaluationModelTypeChat,
				ModelID:     "chat-1",
				ModelName:   "fake-chat",
				Operation:   types.EvaluationOperationChat,
				CallCount:   1,
				ItemCount:   1,
				UsageSource: types.EvaluationUsageSourceProviderReported,
				Usage:       &types.TokenUsage{PromptTokens: index + 1},
			}, nil)
		}(i)
	}
	wg.Wait()

	snapshot := observer.CollectorSnapshot()
	if len(snapshot) != callCount {
		t.Fatalf("record count = %d, want %d", len(snapshot), callCount)
	}
	snapshot[0].Usage.PromptTokens = -1
	secondSnapshot := observer.CollectorSnapshot()
	if secondSnapshot[0].Usage.PromptTokens == -1 {
		t.Fatal("snapshot exposed mutable collector storage")
	}
}

func TestRecordModelCallWithoutObserverIsNoOp(t *testing.T) {
	RecordModelCall(context.Background(), ModelCallRecord{}, nil)
}
