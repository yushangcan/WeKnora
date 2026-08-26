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
	embeddingSnapshot := snapshotModel(embeddingModel)
	chatSnapshot := snapshotModel(chatModel)
	result := &types.EvaluationRunConfig{
		SchemaVersion:         types.EvaluationConfigSchemaVersion,
		Dataset:               dataset,
		SourceKnowledgeBaseID: sourceKnowledgeBaseID,
		Models: types.EvaluationModelConfigSet{
			Embedding: embeddingSnapshot,
			Chat:      chatSnapshot,
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
		Reproducibility: reproducibilitySnapshot(
			dataset,
			commitAvailable,
			vcsModified,
			embeddingModel,
			chatModel,
			rerankModel,
		),
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
	safeExtraConfig, _ := safeModelExtraConfig(model.Type, model.Parameters.ExtraConfig)
	params := struct {
		InterfaceType       string                    `json:"interface_type"`
		EmbeddingParameters types.EmbeddingParameters `json:"embedding_parameters"`
		ParameterSize       string                    `json:"parameter_size"`
		Provider            string                    `json:"provider"`
		SupportsVision      bool                      `json:"supports_vision"`
		MaxConcurrency      int                       `json:"max_concurrency"`
		ExtraConfig         map[string]string         `json:"extra_config,omitempty"`
	}{
		InterfaceType:       model.Parameters.InterfaceType,
		EmbeddingParameters: model.Parameters.EmbeddingParameters,
		ParameterSize:       model.Parameters.ParameterSize,
		Provider:            model.Parameters.Provider,
		SupportsVision:      model.Parameters.SupportsVision,
		MaxConcurrency:      model.Parameters.MaxConcurrency,
		ExtraConfig:         safeExtraConfig,
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

func reproducibilitySnapshot(
	dataset types.EvaluationDatasetDescriptor,
	commitAvailable bool,
	vcsModified bool,
	models ...*types.Model,
) types.EvaluationReproducibility {
	warnings := make([]types.EvaluationWarning, 0)
	if len(dataset.Files) == 0 {
		warnings = append(warnings, types.EvaluationWarning{
			Code:    "dataset_artifact_not_archived",
			Message: "The evaluation dataset does not include a file-level artifact manifest.",
		})
	}
	if !commitAvailable {
		warnings = append(warnings, types.EvaluationWarning{
			Code:    "commit_revision_unavailable",
			Message: "The application build does not report a source commit revision.",
		})
	}
	if vcsModified {
		warnings = append(warnings, types.EvaluationWarning{
			Code:    "working_tree_modified",
			Message: "The application was built from a modified working tree whose diff is not stored.",
		})
	}

	providerConfigExcluded := false
	customHeadersExcluded := false
	for _, model := range models {
		if model == nil {
			continue
		}
		parameters := model.Parameters
		if strings.TrimSpace(parameters.APIKey) != "" ||
			strings.TrimSpace(parameters.AppID) != "" ||
			strings.TrimSpace(parameters.AppSecret) != "" {
			providerConfigExcluded = true
		}
		_, excludedExtraConfig := safeModelExtraConfig(model.Type, parameters.ExtraConfig)
		providerConfigExcluded = providerConfigExcluded || excludedExtraConfig
		customHeadersExcluded = customHeadersExcluded || hasNonEmptyMapValue(parameters.CustomHeaders)
	}
	if providerConfigExcluded {
		warnings = append(warnings, types.EvaluationWarning{
			Code:    "provider_config_partially_snapshotted",
			Message: "One or more provider settings were excluded from the non-secret model snapshot.",
		})
	}
	if customHeadersExcluded {
		warnings = append(warnings, types.EvaluationWarning{
			Code:    "custom_headers_excluded",
			Message: "Custom model request headers were excluded from the evaluation configuration.",
		})
	}

	status := types.EvaluationReproducibilityComplete
	if len(warnings) > 0 {
		status = types.EvaluationReproducibilityPartial
	}
	return types.EvaluationReproducibility{Status: status, Warnings: warnings}
}

func safeModelExtraConfig(modelType types.ModelType, extraConfig map[string]string) (map[string]string, bool) {
	allowedKeys := map[string]struct{}{}
	switch modelType {
	case types.ModelTypeKnowledgeQA:
		allowedKeys = map[string]struct{}{
			"api_version": {}, "remote_model_name": {}, "thinking_control": {},
		}
	case types.ModelTypeEmbedding:
		allowedKeys = map[string]struct{}{
			"api_version": {}, "remote_model_name": {},
		}
	case types.ModelTypeRerank:
		allowedKeys = map[string]struct{}{
			"instruction": {}, "region": {}, "remote_model_name": {}, "truncate_prompt_tokens": {},
		}
	}

	safe := make(map[string]string)
	excluded := false
	for key, value := range extraConfig {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, ok := allowedKeys[key]; !ok {
			excluded = true
			continue
		}
		safe[key] = value
	}
	if len(safe) == 0 {
		safe = nil
	}
	return safe, excluded
}

func hasNonEmptyMapValue(values map[string]string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func fingerprintText(value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}
