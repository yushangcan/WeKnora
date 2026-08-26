package evaluation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// NewRunConfig builds a deterministic, non-secret snapshot of effective run inputs.
func NewRunConfig(
	dataset types.EvaluationDatasetDescriptor,
	sourceKnowledgeBaseID string,
	kb *types.KnowledgeBase,
	embeddingModel *types.Model,
	chatModel *types.Model,
	rerankModel *types.Model,
	params *types.ChatManage,
	caseConcurrency int,
	applicationVersion string,
) (*types.EvaluationRunConfig, error) {
	if kb == nil || embeddingModel == nil || chatModel == nil || params == nil {
		return nil, fmt.Errorf("evaluation run configuration is incomplete")
	}
	commitSHA, vcsModified, commitAvailable := currentVCSIdentity()
	result := &types.EvaluationRunConfig{
		SchemaVersion:         types.EvaluationConfigSchemaVersion,
		Dataset:               dataset,
		SourceKnowledgeBaseID: sourceKnowledgeBaseID,
		Models: types.EvaluationModelConfigSet{
			Embedding: snapshotModel(embeddingModel),
			Chat:      snapshotModel(chatModel),
		},
		Chunking: types.EvaluationChunkingConfig{
			Applied:    true,
			SourceUnit: "dataset_passage",
			Config: types.EvaluationChunkingParameters{
				ChunkSize:         kb.ChunkingConfig.ChunkSize,
				ChunkOverlap:      kb.ChunkingConfig.ChunkOverlap,
				Separators:        append([]string(nil), kb.ChunkingConfig.Separators...),
				EnableParentChild: kb.ChunkingConfig.EnableParentChild,
				ParentChunkSize:   kb.ChunkingConfig.ParentChunkSize,
				ChildChunkSize:    kb.ChunkingConfig.ChildChunkSize,
				Strategy:          kb.ChunkingConfig.Strategy,
				TokenLimit:        kb.ChunkingConfig.TokenLimit,
				Languages:         append([]string(nil), kb.ChunkingConfig.Languages...),
			},
		},
		Retrieval: types.EvaluationRetrievalConfig{
			VectorThreshold:  params.VectorThreshold,
			KeywordThreshold: params.KeywordThreshold,
			EmbeddingTopK:    params.EmbeddingTopK,
			RerankTopK:       params.RerankTopK,
			RerankThreshold:  params.RerankThreshold,
		},
		Generation: types.EvaluationGenerationConfig{
			MaxTokens:                   params.SummaryConfig.MaxTokens,
			MaxCompletionTokens:         params.SummaryConfig.MaxCompletionTokens,
			Temperature:                 params.SummaryConfig.Temperature,
			TopP:                        params.SummaryConfig.TopP,
			TopK:                        params.SummaryConfig.TopK,
			Seed:                        params.SummaryConfig.Seed,
			RepeatPenalty:               params.SummaryConfig.RepeatPenalty,
			FrequencyPenalty:            params.SummaryConfig.FrequencyPenalty,
			PresencePenalty:             params.SummaryConfig.PresencePenalty,
			Thinking:                    params.SummaryConfig.Thinking,
			PromptFingerprint:           fingerprintText(params.SummaryConfig.Prompt),
			ContextFingerprint:          fingerprintText(params.SummaryConfig.ContextTemplate),
			NoMatchPrefixFingerprint:    fingerprintText(params.SummaryConfig.NoMatchPrefix),
			FallbackResponseFingerprint: fingerprintText(params.FallbackResponse),
			FallbackPromptFingerprint:   fingerprintText(params.FallbackPrompt),
		},
		Indexing: types.EvaluationIndexingConfig{
			VectorEnabled:  kb.IndexingStrategy.VectorEnabled,
			KeywordEnabled: kb.IndexingStrategy.KeywordEnabled,
		},
		Runtime: types.EvaluationRuntimeConfig{
			CaseConcurrency:    caseConcurrency,
			MetricVersion:      types.EvaluationMetricVersion,
			ResultVersion:      types.EvaluationResultSchemaVersion,
			ApplicationVersion: strings.TrimSpace(applicationVersion),
			CommitSHA:          commitSHA,
			VCSModified:        vcsModified,
			CommitAvailable:    commitAvailable,
		},
	}
	if kb.VectorStoreID != nil {
		result.Indexing.VectorStoreID = *kb.VectorStoreID
	}
	if rerankModel != nil {
		snapshot := snapshotModel(rerankModel)
		result.Models.Rerank = &snapshot
	}

	hash, err := ConfigHash(result)
	if err != nil {
		return nil, err
	}
	result.ConfigHash = hash
	return result, nil
}

func currentVCSIdentity() (string, bool, bool) {
	return vcsIdentityFromBuildInfo(debug.ReadBuildInfo())
}

func vcsIdentityFromBuildInfo(info *debug.BuildInfo, ok bool) (string, bool, bool) {
	if !ok || info == nil {
		return "unknown", false, false
	}
	commitSHA := ""
	vcsModified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			commitSHA = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			vcsModified = setting.Value == "true"
		}
	}
	if commitSHA == "" {
		return "unknown", vcsModified, false
	}
	return commitSHA, vcsModified, true
}

// ConfigHash returns the SHA-256 hash of a canonical run configuration.
func ConfigHash(config *types.EvaluationRunConfig) (string, error) {
	if config == nil {
		return "", fmt.Errorf("evaluation run configuration is nil")
	}
	copy := *config
	copy.ConfigHash = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", fmt.Errorf("marshal evaluation run configuration: %w", err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

// SafeParamsSnapshot keeps legacy numeric and model fields without prompt text.
func SafeParamsSnapshot(params *types.ChatManage) *types.ChatManage {
	if params == nil {
		return nil
	}
	snapshot := params.Clone()
	snapshot.SummaryConfig.Prompt = ""
	snapshot.SummaryConfig.ContextTemplate = ""
	snapshot.SummaryConfig.NoMatchPrefix = ""
	snapshot.FallbackResponse = ""
	snapshot.FallbackPrompt = ""
	snapshot.RewritePromptSystem = ""
	snapshot.RewritePromptUser = ""
	return snapshot
}

func snapshotModel(model *types.Model) types.EvaluationModelConfig {
	params := struct {
		InterfaceType       string                    `json:"interface_type"`
		EmbeddingParameters types.EmbeddingParameters `json:"embedding_parameters"`
		ParameterSize       string                    `json:"parameter_size"`
		Provider            string                    `json:"provider"`
		SupportsVision      bool                      `json:"supports_vision"`
		MaxConcurrency      int                       `json:"max_concurrency"`
	}{
		InterfaceType:       model.Parameters.InterfaceType,
		EmbeddingParameters: model.Parameters.EmbeddingParameters,
		ParameterSize:       model.Parameters.ParameterSize,
		Provider:            model.Parameters.Provider,
		SupportsVision:      model.Parameters.SupportsVision,
		MaxConcurrency:      model.Parameters.MaxConcurrency,
	}
	parameterData, _ := json.Marshal(params)
	return types.EvaluationModelConfig{
		ID:                    model.ID,
		Name:                  model.Name,
		DisplayName:           model.DisplayName,
		Type:                  model.Type,
		Source:                model.Source,
		Provider:              model.Parameters.Provider,
		InterfaceType:         model.Parameters.InterfaceType,
		EmbeddingDimension:    model.Parameters.EmbeddingParameters.Dimension,
		MaxConcurrency:        model.Parameters.MaxConcurrency,
		EndpointFingerprint:   fingerprintText(model.Parameters.BaseURL),
		ParametersFingerprint: fmt.Sprintf("sha256:%x", sha256.Sum256(parameterData)),
		UpdatedAt:             model.UpdatedAt,
	}
}

func fingerprintText(value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}
