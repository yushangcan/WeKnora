package chat

import (
	"context"
	"time"

	evaluationobs "github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/types"
)

// evaluationMeterChat observes non-streaming calls only when the call context
// belongs to an evaluation run. It does not alter provider usage generation.
type evaluationMeterChat struct {
	inner Chat
}

func WrapEvaluationMeter(inner Chat) Chat {
	if inner == nil {
		return nil
	}
	return &evaluationMeterChat{inner: inner}
}

func (m *evaluationMeterChat) Chat(
	ctx context.Context,
	messages []Message,
	opts *ChatOptions,
) (*types.ChatResponse, error) {
	if evaluationobs.ObserverFromContext(ctx) == nil {
		return m.inner.Chat(ctx, messages, opts)
	}
	startedAt := time.Now()
	response, err := m.inner.Chat(ctx, messages, opts)

	var usage *types.TokenUsage
	usageSource := types.EvaluationUsageSourceUnavailable
	if response != nil {
		usageCopy := response.Usage
		usage = &usageCopy
		if hasReportedTokenUsage(usageCopy) {
			usageSource = types.EvaluationUsageSourceProviderReported
		}
	}
	evaluationobs.RecordModelCall(ctx, evaluationobs.ModelCallRecord{
		ModelType:   types.EvaluationModelTypeChat,
		ModelID:     m.inner.GetModelID(),
		ModelName:   m.inner.GetModelName(),
		Operation:   types.EvaluationOperationChat,
		DurationMS:  time.Since(startedAt).Milliseconds(),
		CallCount:   1,
		ItemCount:   1,
		UsageSource: usageSource,
		Usage:       usage,
	}, err)
	return response, err
}

// Stage one intentionally does not add stream aggregation. The existing
// stream behavior and usage handling are delegated unchanged.
func (m *evaluationMeterChat) ChatStream(
	ctx context.Context,
	messages []Message,
	opts *ChatOptions,
) (<-chan types.StreamResponse, error) {
	return m.inner.ChatStream(ctx, messages, opts)
}

func (m *evaluationMeterChat) GetModelName() string { return m.inner.GetModelName() }
func (m *evaluationMeterChat) GetModelID() string   { return m.inner.GetModelID() }

func hasReportedTokenUsage(usage types.TokenUsage) bool {
	return usage.PromptTokens != 0 ||
		usage.CompletionTokens != 0 ||
		usage.TotalTokens != 0 ||
		usage.CachedTokens != 0 ||
		usage.CacheReadTokens != 0 ||
		usage.CacheWriteTokens != 0 ||
		usage.CacheMissTokens != 0
}
