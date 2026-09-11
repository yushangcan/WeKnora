package usage

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/models/asr"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// WrapVLM records one event for each image understanding round-trip.
func WrapVLM(inner vlm.VLM, recorder interfaces.ModelUsageRecorder, metadata ModelMetadata) vlm.VLM {
	if inner == nil || recorder == nil {
		return inner
	}
	return &vlmRecorder{inner: inner, recorder: recorder, metadata: metadata}
}

type vlmRecorder struct {
	inner    vlm.VLM
	recorder interfaces.ModelUsageRecorder
	metadata ModelMetadata
}

func (w *vlmRecorder) Predict(ctx context.Context, images [][]byte, prompt string) (string, error) {
	startedAt := time.Now()
	capture := &providerUsageCapture{}
	result, err := w.inner.Predict(types.WithProviderUsageSink(ctx, capture), images, prompt)
	event := buildEvent(ctx, w.metadata, "vlm_predict", startedAt, len(images), err == nil, err, nil)
	attachProviderUsage(event, capture.Usage())
	recordEvent(ctx, w.recorder, event)
	return result, err
}

func (w *vlmRecorder) GetModelName() string { return w.inner.GetModelName() }
func (w *vlmRecorder) GetModelID() string   { return w.inner.GetModelID() }

// WrapASR records one event for each audio transcription round-trip. Most ASR
// APIs do not report token usage, so the event intentionally keeps usage nil.
func WrapASR(inner asr.ASR, recorder interfaces.ModelUsageRecorder, metadata ModelMetadata) asr.ASR {
	if inner == nil || recorder == nil {
		return inner
	}
	return &asrRecorder{inner: inner, recorder: recorder, metadata: metadata}
}

type asrRecorder struct {
	inner    asr.ASR
	recorder interfaces.ModelUsageRecorder
	metadata ModelMetadata
}

func (w *asrRecorder) Transcribe(ctx context.Context, audio []byte, fileName string) (*asr.TranscriptionResult, error) {
	startedAt := time.Now()
	result, err := w.inner.Transcribe(ctx, audio, fileName)
	event := buildEvent(ctx, w.metadata, "asr_transcribe", startedAt, 1, err == nil, err, nil)
	recordEvent(ctx, w.recorder, event)
	return result, err
}

func (w *asrRecorder) GetModelName() string { return w.inner.GetModelName() }
func (w *asrRecorder) GetModelID() string   { return w.inner.GetModelID() }
