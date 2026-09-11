package usage

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/asr"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
)

type usageVLMStub struct {
	usage *types.ProviderUsage
	err   error
}

func (s *usageVLMStub) Predict(ctx context.Context, _ [][]byte, _ string) (string, error) {
	types.RecordProviderUsage(ctx, s.usage)
	return "ok", s.err
}
func (*usageVLMStub) GetModelName() string { return "vlm" }
func (*usageVLMStub) GetModelID() string   { return "vlm-id" }

type usageASRStub struct{ err error }

func (s *usageASRStub) Transcribe(context.Context, []byte, string) (*asr.TranscriptionResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &asr.TranscriptionResult{Text: "ok"}, nil
}
func (*usageASRStub) GetModelName() string { return "asr" }
func (*usageASRStub) GetModelID() string   { return "asr-id" }

func TestWrapVLMRecordsProviderUsage(t *testing.T) {
	recorder := &recordingStub{}
	tokens := types.TokenUsage{PromptTokens: 4, TotalTokens: 4}
	wrapped := WrapVLM(&usageVLMStub{usage: &types.ProviderUsage{Tokens: &tokens, RequestID: "vlm-request"}}, recorder, ModelMetadata{
		ModelID: "vlm-id", ModelName: "vlm", ModelType: types.ModelTypeVLLM, Provider: "openai",
	})
	if _, err := wrapped.Predict(usageTestContext(), [][]byte{{1}}, "describe"); err != nil {
		t.Fatal(err)
	}
	if len(recorder.events) != 1 || recorder.events[0].Operation != "vlm_predict" || recorder.events[0].ProviderUsage == nil {
		t.Fatalf("recorded events = %#v", recorder.events)
	}
	if recorder.events[0].ProviderRequestID != "vlm-request" {
		t.Fatalf("request id = %q", recorder.events[0].ProviderRequestID)
	}
}

func TestWrapASRRecordsFailureWithoutInventingUsage(t *testing.T) {
	recorder := &recordingStub{}
	wantErr := errors.New("provider failed")
	wrapped := WrapASR(&usageASRStub{err: wantErr}, recorder, ModelMetadata{
		ModelID: "asr-id", ModelName: "asr", ModelType: types.ModelTypeASR, Provider: "openai",
	})
	if _, err := wrapped.Transcribe(usageTestContext(), []byte{1}, "a.mp3"); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v", err)
	}
	if len(recorder.events) != 1 || recorder.events[0].Operation != "asr_transcribe" || recorder.events[0].Success {
		t.Fatalf("recorded events = %#v", recorder.events)
	}
	if recorder.events[0].TotalTokens != nil || recorder.events[0].CostAmount != nil {
		t.Fatalf("unexpected fabricated usage/cost: %#v", recorder.events[0])
	}
}

var _ vlm.VLM = (*usageVLMStub)(nil)
var _ asr.ASR = (*usageASRStub)(nil)
