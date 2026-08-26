package evaluation

import (
	"encoding/json"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestNewRunConfigIsStableAndExcludesSecrets(t *testing.T) {
	now := time.Date(2026, 8, 25, 1, 2, 3, 0, time.UTC)
	embeddingModel := &types.Model{
		ID: "embedding-1", Name: "embedding", Type: types.ModelTypeEmbedding,
		Source: types.ModelSourceRemote, UpdatedAt: now,
		Parameters: types.ModelParameters{
			BaseURL: "https://private.example/v1", APIKey: "secret-api-key", AppSecret: "secret-app",
			CustomHeaders: map[string]string{"Authorization": "secret-header"},
			ExtraConfig:   map[string]string{"credential": "secret-extra"},
			Provider:      "generic", EmbeddingParameters: types.EmbeddingParameters{Dimension: 1024},
		},
	}
	chatModel := &types.Model{ID: "chat-1", Name: "chat", Type: types.ModelTypeKnowledgeQA, UpdatedAt: now}
	kb := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{
			ChunkSize: 512, ChunkOverlap: 80, TableMetadataInstructions: "private chunk instruction",
		},
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true},
	}
	params := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		VectorThreshold: 0.5, EmbeddingTopK: 10, ChatModelID: chatModel.ID,
		SummaryConfig: types.SummaryConfig{
			Prompt: "private prompt", ContextTemplate: "private context",
			NoMatchPrefix: "private no-match", Seed: 7,
		},
		FallbackResponse: "private fallback response",
		FallbackPrompt:   "private fallback prompt",
	}}
	dataset := types.EvaluationDatasetDescriptor{ID: "default", Version: "1", ContentFingerprint: "sha256:data"}

	first, err := NewRunConfig(dataset, "source-kb", kb, embeddingModel, chatModel, nil, params, 4, "0.7.2")
	if err != nil {
		t.Fatalf("build first config: %v", err)
	}
	second, err := NewRunConfig(dataset, "source-kb", kb, embeddingModel, chatModel, nil, params, 4, "0.7.2")
	if err != nil {
		t.Fatalf("build second config: %v", err)
	}
	if first.ConfigHash != second.ConfigHash {
		t.Fatalf("stable configuration hashes differ: %s != %s", first.ConfigHash, second.ConfigHash)
	}

	encoded := first.String()
	for _, secret := range []string{
		"secret-api-key", "secret-app", "secret-header", "secret-extra", "private.example",
		"private prompt", "private context", "private no-match", "private fallback response",
		"private fallback prompt", "private chunk instruction",
	} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("configuration contains secret value %q", secret)
		}
	}

	changed := *first
	changed.Retrieval.EmbeddingTopK++
	changedHash, err := ConfigHash(&changed)
	if err != nil {
		t.Fatalf("hash changed config: %v", err)
	}
	if changedHash == first.ConfigHash {
		t.Fatal("configuration hash did not change after an effective parameter changed")
	}
}

func TestVCSIdentityFromBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc123"},
		{Key: "vcs.modified", Value: "true"},
	}}
	commitSHA, modified, available := vcsIdentityFromBuildInfo(info, true)
	if commitSHA != "abc123" || !modified || !available {
		t.Fatalf("unexpected VCS identity: sha=%q modified=%v available=%v", commitSHA, modified, available)
	}

	commitSHA, modified, available = vcsIdentityFromBuildInfo(nil, false)
	if commitSHA != "unknown" || modified || available {
		t.Fatalf("unavailable VCS identity was not explicit: sha=%q modified=%v available=%v", commitSHA, modified, available)
	}
}

func TestSafeParamsSnapshotRemovesPromptText(t *testing.T) {
	params := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		EmbeddingTopK:       10,
		FallbackResponse:    "private fallback",
		FallbackPrompt:      "private fallback prompt",
		RewritePromptSystem: "private rewrite system",
		RewritePromptUser:   "private rewrite user",
		SummaryConfig: types.SummaryConfig{
			Prompt: "private prompt", ContextTemplate: "private context", NoMatchPrefix: "private prefix",
		},
	}}

	snapshot := SafeParamsSnapshot(params)
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal safe params: %v", err)
	}
	encoded := string(data)
	for _, text := range []string{
		"private fallback", "private fallback prompt", "private rewrite system",
		"private rewrite user", "private prompt", "private context", "private prefix",
	} {
		if strings.Contains(encoded, text) {
			t.Fatalf("safe params contains prompt text %q", text)
		}
	}
	if snapshot.EmbeddingTopK != 10 {
		t.Fatalf("safe params lost numeric configuration: %#v", snapshot)
	}
	if params.SummaryConfig.Prompt != "private prompt" {
		t.Fatal("safe params mutated the runtime configuration")
	}
}
