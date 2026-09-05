package usage

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type chatRecorder struct {
	inner    chat.Chat
	recorder interfaces.ModelUsageRecorder
	metadata ModelMetadata
}

// WrapChat adds one durable event around each real Chat or ChatStream call.
func WrapChat(inner chat.Chat, recorder interfaces.ModelUsageRecorder, metadata ModelMetadata) chat.Chat {
	if inner == nil || recorder == nil {
		return inner
	}
	return &chatRecorder{inner: inner, recorder: recorder, metadata: metadata}
}

func (w *chatRecorder) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	startedAt := time.Now()
	response, err := w.inner.Chat(ctx, messages, opts)
	var usage *types.TokenUsage
	if response != nil && hasReportedTokenUsage(response.Usage) {
		copy := response.Usage
		usage = &copy
	}
	event := buildEvent(ctx, w.metadata, "chat", startedAt, 1, err == nil, err, usage)
	recordEvent(ctx, w.recorder, event)
	return response, err
}

func (w *chatRecorder) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	startedAt := time.Now()
	in, err := w.inner.ChatStream(ctx, messages, opts)
	if err != nil || in == nil {
		if err == nil {
			err = errors.New("chat stream returned a nil channel")
		}
		recordEvent(ctx, w.recorder, buildEvent(ctx, w.metadata, "chat_stream", startedAt, 1, false, err, nil))
		return in, err
	}
	out := make(chan types.StreamResponse)
	go func() {
		defer close(out)
		var usage *types.TokenUsage
		var streamErr error
		for {
			select {
			case response, ok := <-in:
				if !ok {
					recordEvent(ctx, w.recorder, buildEvent(ctx, w.metadata, "chat_stream", startedAt, 1, streamErr == nil, streamErr, usage))
					return
				}
				if response.Usage != nil && hasReportedTokenUsage(*response.Usage) {
					copy := *response.Usage
					usage = &copy
				}
				if response.ResponseType == types.ResponseTypeError && streamErr == nil {
					message := response.Content
					if message == "" {
						message = "chat stream returned an error"
					}
					streamErr = errors.New(message)
				}
				select {
				case out <- response:
				case <-ctx.Done():
					recordEvent(ctx, w.recorder, buildEvent(ctx, w.metadata, "chat_stream", startedAt, 1, false, ctx.Err(), usage))
					go func() {
						for range in {
						}
					}()
					return
				}
			case <-ctx.Done():
				recordEvent(ctx, w.recorder, buildEvent(ctx, w.metadata, "chat_stream", startedAt, 1, false, ctx.Err(), usage))
				go func() {
					for range in {
					}
				}()
				return
			}
		}
	}()
	return out, nil
}

func (w *chatRecorder) GetModelName() string { return w.inner.GetModelName() }
func (w *chatRecorder) GetModelID() string   { return w.inner.GetModelID() }

type embeddingRecorder struct {
	inner    embedding.Embedder
	recorder interfaces.ModelUsageRecorder
	metadata ModelMetadata
}

// WrapEmbedding records one event per logical embedding request. Pool
// sub-batches are delegated to the inner implementation and are not recorded.
func WrapEmbedding(inner embedding.Embedder, recorder interfaces.ModelUsageRecorder, metadata ModelMetadata) embedding.Embedder {
	if inner == nil || recorder == nil {
		return inner
	}
	return &embeddingRecorder{inner: inner, recorder: recorder, metadata: metadata}
}

func (w *embeddingRecorder) Embed(ctx context.Context, text string) ([]float32, error) {
	startedAt := time.Now()
	result, err := w.inner.Embed(ctx, text)
	if !embedding.IsEmbeddingPoolSubcall(ctx) {
		recordEvent(ctx, w.recorder, buildEvent(ctx, w.metadata, "embed", startedAt, 1, err == nil, err, nil))
	}
	return result, err
}

func (w *embeddingRecorder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	startedAt := time.Now()
	result, err := w.inner.BatchEmbed(ctx, texts)
	if !embedding.IsEmbeddingPoolSubcall(ctx) {
		recordEvent(ctx, w.recorder, buildEvent(ctx, w.metadata, "batch_embed", startedAt, len(texts), err == nil, err, nil))
	}
	return result, err
}

func (w *embeddingRecorder) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, texts []string) ([][]float32, error) {
	startedAt := time.Now()
	poolModel := model
	if poolModel == nil || poolModel == w {
		poolModel = w.inner
	}
	result, err := w.inner.BatchEmbedWithPool(ctx, poolModel, texts)
	recordEvent(ctx, w.recorder, buildEvent(ctx, w.metadata, "batch_embed", startedAt, len(texts), err == nil, err, nil))
	return result, err
}

func (w *embeddingRecorder) GetModelName() string { return w.inner.GetModelName() }
func (w *embeddingRecorder) GetDimensions() int   { return w.inner.GetDimensions() }
func (w *embeddingRecorder) GetModelID() string   { return w.inner.GetModelID() }

type rerankRecorder struct {
	inner    rerank.Reranker
	recorder interfaces.ModelUsageRecorder
	metadata ModelMetadata
}

// WrapRerank records one event for each rerank request and preserves the
// original ranked results and errors.
func WrapRerank(inner rerank.Reranker, recorder interfaces.ModelUsageRecorder, metadata ModelMetadata) rerank.Reranker {
	if inner == nil || recorder == nil {
		return inner
	}
	return &rerankRecorder{inner: inner, recorder: recorder, metadata: metadata}
}

func (w *rerankRecorder) Rerank(ctx context.Context, query string, documents []string) ([]rerank.RankResult, error) {
	startedAt := time.Now()
	result, err := w.inner.Rerank(ctx, query, documents)
	recordEvent(ctx, w.recorder, buildEvent(ctx, w.metadata, "rerank", startedAt, len(documents), err == nil, err, nil))
	return result, err
}

func (w *rerankRecorder) GetModelName() string { return w.inner.GetModelName() }
func (w *rerankRecorder) GetModelID() string   { return w.inner.GetModelID() }
