package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	evaluationobs "github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/types"
)

type evaluationMeterFakeChat struct {
	response *types.ChatResponse
	err      error
	calls    int
}

func (f *evaluationMeterFakeChat) Chat(
	_ context.Context,
	_ []Message,
	_ *ChatOptions,
) (*types.ChatResponse, error) {
	f.calls++
	return f.response, f.err
}

func (f *evaluationMeterFakeChat) ChatStream(
	_ context.Context,
	_ []Message,
	_ *ChatOptions,
) (<-chan types.StreamResponse, error) {
	return nil, f.err
}

func (f *evaluationMeterFakeChat) GetModelName() string { return "fake-chat" }
func (f *evaluationMeterFakeChat) GetModelID() string   { return "chat-1" }

func TestEvaluationChatMeterRecordsProviderUsage(t *testing.T) {
	inner := &evaluationMeterFakeChat{response: &types.ChatResponse{
		Content: "same response",
		Usage: types.TokenUsage{
			PromptTokens:     5,
			CompletionTokens: 2,
			TotalTokens:      7,
			CacheReadTokens:  3,
			CacheReported:    true,
			CacheStatus:      types.PromptCacheStatusHit,
		},
	}}
	metered := WrapEvaluationMeter(inner)
	observer := evaluationobs.NewObserver("run-1", 1, "default", time.Now())
	ctx := evaluationobs.WithEvaluationCase(
		evaluationobs.WithEvaluationRun(context.Background(), observer),
		"case-1",
	)

	response, err := metered.Chat(ctx, []Message{{Role: "user", Content: "secret"}}, nil)
	if err != nil || response != inner.response {
		t.Fatalf("wrapper changed response or error: response=%p err=%v", response, err)
	}
	records := observer.CollectorSnapshot()
	if len(records) != 1 || records[0].UsageSource != types.EvaluationUsageSourceProviderReported {
		t.Fatalf("unexpected records: %#v", records)
	}
	if records[0].Usage.TotalTokens != 7 || records[0].Usage.CacheStatus != types.PromptCacheStatusHit {
		t.Fatalf("usage was not copied unchanged: %#v", records[0].Usage)
	}
}

func TestEvaluationChatMeterRecordsFailureAndSkipsPlainContext(t *testing.T) {
	expectedErr := errors.New("provider unavailable")
	inner := &evaluationMeterFakeChat{err: expectedErr}
	metered := WrapEvaluationMeter(inner)
	observer := evaluationobs.NewObserver("run-1", 1, "default", time.Now())
	ctx := evaluationobs.WithEvaluationRun(context.Background(), observer)

	_, err := metered.Chat(ctx, nil, nil)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("wrapper changed error: %v", err)
	}
	if records := observer.CollectorSnapshot(); len(records) != 1 || records[0].Success {
		t.Fatalf("failed call was not recorded: %#v", records)
	}

	_, _ = metered.Chat(context.Background(), nil, nil)
	if got := len(observer.CollectorSnapshot()); got != 1 {
		t.Fatalf("plain context added a record: %d", got)
	}
}
