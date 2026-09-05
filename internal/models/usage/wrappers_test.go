package usage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
)

type recordingStub struct {
	events []*types.ModelUsageEvent
	ctxErr error
}

func (r *recordingStub) Record(ctx context.Context, event *types.ModelUsageEvent) error {
	r.ctxErr = ctx.Err()
	copy := *event
	r.events = append(r.events, &copy)
	return nil
}

type notificationRecorder struct {
	recordingStub
	done chan struct{}
}

func (r *notificationRecorder) Record(ctx context.Context, event *types.ModelUsageEvent) error {
	if err := r.recordingStub.Record(ctx, event); err != nil {
		return err
	}
	select {
	case r.done <- struct{}{}:
	default:
	}
	return nil
}

type stubChat struct {
	response *types.ChatResponse
	err      error
	stream   []types.StreamResponse
}

type channelChat struct {
	stream <-chan types.StreamResponse
}

func (c *channelChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return &types.ChatResponse{}, nil
}
func (c *channelChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return c.stream, nil
}
func (c *channelChat) GetModelName() string { return "chat-name" }
func (c *channelChat) GetModelID() string   { return "chat-id" }

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

type countedEmbedder struct{ calls int }

func (e *countedEmbedder) Embed(context.Context, string) ([]float32, error) {
	e.calls++
	return []float32{1, 2}, nil
}
func (e *countedEmbedder) BatchEmbed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	result := make([][]float32, len(texts))
	for index := range result {
		result[index] = []float32{1, 2}
	}
	return result, nil
}
func (e *countedEmbedder) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, texts []string) ([][]float32, error) {
	return model.BatchEmbed(ctx, texts)
}
func (*countedEmbedder) GetModelName() string { return "counted-embed" }
func (*countedEmbedder) GetDimensions() int   { return 2 }
func (*countedEmbedder) GetModelID() string   { return "counted-embed-id" }

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

func TestWrapChatPropagatesProviderUsageScope(t *testing.T) {
	amount := 0.0123
	response := &types.ChatResponse{
		ProviderUsage: &types.ProviderUsage{
			Amount:        &amount,
			Currency:      "USD",
			BillableUnits: map[string]float64{"input": 120},
			RawSource:     "provider-response",
		},
	}
	recorder := &recordingStub{}
	wrapped := WrapChat(&stubChat{response: response}, recorder, ModelMetadata{
		ModelID: "chat-id", ModelName: "chat-name", ModelType: types.ModelTypeKnowledgeQA,
	})
	if _, err := wrapped.Chat(usageTestContext(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(recorder.events) != 1 || recorder.events[0].ProviderUsage == nil {
		t.Fatalf("provider usage scope = %#v", recorder.events)
	}
	got := recorder.events[0].ProviderUsage
	if got.Currency != "USD" || got.RawSource != "provider-response" || got.Amount == nil || *got.Amount != amount || got.BillableUnits["input"] != 120 {
		t.Fatalf("provider usage scope = %#v", got)
	}
	response.ProviderUsage.BillableUnits["input"] = 999
	if got.BillableUnits["input"] != 120 {
		t.Fatal("usage event retained a mutable provider scope")
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

func TestWrapChatStreamRecordsProviderErrorResponseAsFailure(t *testing.T) {
	recorder := &recordingStub{}
	wrapped := WrapChat(&stubChat{stream: []types.StreamResponse{{
		ResponseType: types.ResponseTypeError,
		Content:      "upstream stream failed",
		Done:         true,
	}}}, recorder, ModelMetadata{
		ModelID: "chat-id", ModelName: "chat-name", ModelType: types.ModelTypeKnowledgeQA,
	})
	stream, err := wrapped.ChatStream(usageTestContext(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	if len(recorder.events) != 1 {
		t.Fatalf("stream events = %d, want 1", len(recorder.events))
	}
	if recorder.events[0].Success || recorder.events[0].ErrorMessage == "" {
		t.Fatalf("provider error event = %#v", recorder.events[0])
	}
}

func TestWrapChatStreamRecordsCancellationOnce(t *testing.T) {
	in := make(chan types.StreamResponse)
	recorder := &notificationRecorder{done: make(chan struct{}, 1)}
	wrapped := WrapChat(&channelChat{stream: in}, recorder, ModelMetadata{
		ModelID: "chat-id", ModelName: "chat-name", ModelType: types.ModelTypeKnowledgeQA,
	})
	ctx, cancel := context.WithCancel(usageTestContext())
	stream, err := wrapped.ChatStream(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-recorder.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancellation event")
	}
	close(in)
	for range stream {
	}
	if len(recorder.events) != 1 || recorder.events[0].Success {
		t.Fatalf("cancellation events = %#v", recorder.events)
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

func TestWrapChatRedactsProviderCredentialsInFailureEvent(t *testing.T) {
	recorder := &recordingStub{}
	wrapped := WrapChat(&stubChat{err: errors.New("request failed api_key=sk-test authorization=Bearer secret")}, recorder, ModelMetadata{
		ModelID: "chat-id", ModelName: "chat-name", ModelType: types.ModelTypeKnowledgeQA,
	})
	_, _ = wrapped.Chat(usageTestContext(), nil, nil)
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	message := recorder.events[0].ErrorMessage
	if strings.Contains(message, "sk-test") || strings.Contains(message, "Bearer secret") {
		t.Fatalf("failure event leaked credentials: %q", message)
	}
}

func TestRecordEventPersistsAfterRequestCancellation(t *testing.T) {
	recorder := &recordingStub{}
	ctx, cancel := context.WithCancel(usageTestContext())
	cancel()

	wrapped := WrapChat(&stubChat{response: &types.ChatResponse{}}, recorder, ModelMetadata{
		ModelID: "chat-id", ModelName: "chat-name", ModelType: types.ModelTypeKnowledgeQA,
	})
	_, err := wrapped.Chat(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	if recorder.events[0].TotalTokens != nil {
		t.Fatalf("unreported token usage = %#v, want nil", recorder.events[0].TotalTokens)
	}
	if recorder.ctxErr != nil {
		t.Fatalf("recorder context err = %v, want nil", recorder.ctxErr)
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

func TestEmbeddingResultCacheHitDoesNotRecordProviderUsage(t *testing.T) {
	t.Setenv("WEKNORA_EMBEDDING_CACHE_ENABLED", "true")
	inner := &countedEmbedder{}
	recorder := &recordingStub{}
	observed := WrapEmbedding(inner, recorder, ModelMetadata{
		ModelID: "counted-embed-id", ModelName: "counted-embed", ModelType: types.ModelTypeEmbedding,
	})
	cached := embedding.WrapResultCache(observed, embedding.NewEmbeddingResultCache(nil), embedding.Config{
		ModelID: "counted-embed-id", ModelName: "counted-embed", Dimensions: 2,
	}, 7)
	if _, err := cached.Embed(usageTestContext(), "same"); err != nil {
		t.Fatal(err)
	}
	if _, err := cached.Embed(usageTestContext(), "same"); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 || len(recorder.events) != 1 {
		t.Fatalf("provider calls=%d usage events=%d, want one each", inner.calls, len(recorder.events))
	}
}

func TestEmbeddingResultCachePooledMissRecordsOneUsageEvent(t *testing.T) {
	t.Setenv("WEKNORA_EMBEDDING_CACHE_ENABLED", "true")
	inner := &countedEmbedder{}
	recorder := &recordingStub{}
	observed := WrapEmbedding(inner, recorder, ModelMetadata{
		ModelID: "counted-embed-id", ModelName: "counted-embed", ModelType: types.ModelTypeEmbedding,
	})
	cached := embedding.WrapResultCache(observed, embedding.NewEmbeddingResultCache(nil), embedding.Config{
		ModelID: "counted-embed-id", ModelName: "counted-embed", Dimensions: 2,
	}, 7)
	if _, err := cached.BatchEmbedWithPool(usageTestContext(), cached, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cached.BatchEmbedWithPool(usageTestContext(), cached, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 || len(recorder.events) != 1 {
		t.Fatalf("pooled provider calls=%d usage events=%d, want one each", inner.calls, len(recorder.events))
	}
}

func TestBuildEventUsesEvaluationScopeAndUnknownCost(t *testing.T) {
	started := time.Now()
	evalCtx := evaluation.WithEvaluationRun(usageTestContext(), evaluation.NewObserver("run-1", 7, "dataset", started))
	event := buildEvent(evalCtx, ModelMetadata{ModelID: "rerank-id", ModelName: "rerank", ModelType: types.ModelTypeRerank}, "rerank", started, 4, true, nil, nil)
	if event == nil || event.CostAmount != nil || event.CacheReported || event.ItemCount != 4 {
		t.Fatalf("event = %#v", event)
	}
	if event.Source != types.ModelUsageSourceEvaluation || event.EvaluationRunID != "run-1" {
		t.Fatalf("evaluation event attribution = %#v", event)
	}
}

func TestSourceForPurposeClassifiesDocumentProcessingCalls(t *testing.T) {
	for _, purpose := range []string{"ingestion", "document_ingestion", "document_parse", "document_summary", "question_generation", "document_auto_tag", "auto_tag"} {
		t.Run(purpose, func(t *testing.T) {
			if got := sourceForPurpose(purpose); got != types.ModelUsageSourceIngestion {
				t.Fatalf("sourceForPurpose(%q) = %q, want %q", purpose, got, types.ModelUsageSourceIngestion)
			}
		})
	}
}

func TestSourceForPurposeClassifiesAllWikiCalls(t *testing.T) {
	for _, purpose := range []string{
		"wiki", "wiki_ingest", "wiki_page_modify", "wiki_chunk_citation",
		"wiki_candidate_slug", "wiki_summary", "wiki_knowledge_extract",
		"wiki_taxonomy_plan", "wiki_deduplication", "wiki_index_intro", "wiki_generation",
	} {
		t.Run(purpose, func(t *testing.T) {
			if got := sourceForPurpose(purpose); got != types.ModelUsageSourceWiki {
				t.Fatalf("sourceForPurpose(%q) = %q, want %q", purpose, got, types.ModelUsageSourceWiki)
			}
		})
	}
	if got := sourceForPurpose("chat"); got != types.ModelUsageSourceChat {
		t.Fatalf("sourceForPurpose(chat) = %q, want %q", got, types.ModelUsageSourceChat)
	}
}

var _ chat.Chat = (*stubChat)(nil)
var _ embedding.Embedder = stubEmbedder{}
var _ rerank.Reranker = stubReranker{}
