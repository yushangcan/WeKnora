package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
)

type recordingStub struct {
	events []*types.ModelUsageEvent
}

func (r *recordingStub) Record(_ context.Context, event *types.ModelUsageEvent) error {
	copy := *event
	r.events = append(r.events, &copy)
	return nil
}

type stubChat struct {
	response *types.ChatResponse
	err      error
	stream   []types.StreamResponse
}

func (s *stubChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return s.response, s.err
}
func (s *stubChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	ch := make(chan types.StreamResponse, len(s.stream))
	for _, item := range s.stream {
		ch <- item
	}
	close(ch)
	return ch, s.err
}
func (s *stubChat) GetModelName() string { return "chat-name" }
func (s *stubChat) GetModelID() string   { return "chat-id" }

type stubEmbedder struct{}

func (stubEmbedder) Embed(context.Context, string) ([]float32, error) { return []float32{1}, nil }
func (stubEmbedder) BatchEmbed(_ context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}
func (stubEmbedder) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, texts []string) ([][]float32, error) {
	return model.BatchEmbed(ctx, texts)
}
func (stubEmbedder) GetModelName() string { return "embed-name" }
func (stubEmbedder) GetDimensions() int   { return 1 }
func (stubEmbedder) GetModelID() string   { return "embed-id" }

type stubReranker struct{}

func (stubReranker) Rerank(context.Context, string, []string) ([]rerank.RankResult, error) {
	return nil, nil
}
func (stubReranker) GetModelName() string { return "rerank-name" }
func (stubReranker) GetModelID() string   { return "rerank-id" }

func usageTestContext() context.Context {
	return context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
}

func TestWrapChatRecordsSuccessAndProviderUsage(t *testing.T) {
	usage := types.TokenUsage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13}
	recorder := &recordingStub{}
	wrapped := WrapChat(&stubChat{response: &types.ChatResponse{Usage: usage}}, recorder, ModelMetadata{
		ModelID: "chat-id", ModelName: "chat-name", ModelType: types.ModelTypeKnowledgeQA, Provider: "openai",
	})
	response, err := wrapped.Chat(usageTestContext(), nil, nil)
	if err != nil || response == nil {
		t.Fatalf("chat result = %#v, err=%v", response, err)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if !event.Success || event.PromptTokens == nil || *event.PromptTokens != 10 || event.Provider != "openai" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestWrapChatStreamRecordsOneEventAfterChannelCloses(t *testing.T) {
	readUsage := types.TokenUsage{PromptTokens: 8, CompletionTokens: 2, TotalTokens: 10}
	recorder := &recordingStub{}
	wrapped := WrapChat(&stubChat{stream: []types.StreamResponse{{Usage: &readUsage}, {Done: true}}}, recorder, ModelMetadata{
		ModelID: "chat-id", ModelName: "chat-name", ModelType: types.ModelTypeKnowledgeQA,
	})
	stream, err := wrapped.ChatStream(usageTestContext(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	if len(recorder.events) != 1 || !recorder.events[0].Success {
		t.Fatalf("stream events = %#v", recorder.events)
	}
	if recorder.events[0].TotalTokens == nil || *recorder.events[0].TotalTokens != 10 {
		t.Fatalf("stream usage = %#v", recorder.events[0])
	}
}

func TestWrapChatRecordsProviderFailureWithoutChangingError(t *testing.T) {
	wantErr := errors.New("provider failed")
	recorder := &recordingStub{}
	wrapped := WrapChat(&stubChat{err: wantErr}, recorder, ModelMetadata{
		ModelID: "chat-id", ModelName: "chat-name", ModelType: types.ModelTypeKnowledgeQA,
	})
	_, err := wrapped.Chat(usageTestContext(), nil, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want original error", err)
	}
	if len(recorder.events) != 1 || recorder.events[0].Success || recorder.events[0].ErrorMessage == "" {
		t.Fatalf("failure event = %#v", recorder.events)
	}
}

func TestWrapEmbeddingPoolRecordsLogicalRequestOnce(t *testing.T) {
	recorder := &recordingStub{}
	wrapped := WrapEmbedding(stubEmbedder{}, recorder, ModelMetadata{ModelID: "embed-id", ModelName: "embed-name", ModelType: types.ModelTypeEmbedding})
	_, err := wrapped.BatchEmbedWithPool(usageTestContext(), wrapped, []string{"a", "b", "c"})
	if err != nil || len(recorder.events) != 1 || recorder.events[0].ItemCount != 3 {
		t.Fatalf("pool events = %#v, err=%v", recorder.events, err)
	}
}

func TestBuildEventUsesEvaluationScopeAndUnknownCost(t *testing.T) {
	started := time.Now()
	event := buildEvent(usageTestContext(), ModelMetadata{ModelID: "rerank-id", ModelName: "rerank", ModelType: types.ModelTypeRerank}, "rerank", started, 4, true, nil, nil)
	if event == nil || event.CostAmount != nil || event.CacheReported || event.ItemCount != 4 {
		t.Fatalf("event = %#v", event)
	}
}

var _ chat.Chat = (*stubChat)(nil)
var _ embedding.Embedder = stubEmbedder{}
var _ rerank.Reranker = stubReranker{}
