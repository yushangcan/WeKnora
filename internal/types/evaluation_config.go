package types

import (
	"encoding/json"
	"time"
)

const (
	EvaluationConfigSchemaVersion = "evaluation-config/v1"
	EvaluationMetricVersion       = "retrieval-generation/v1"

	EvaluationDatasetModePassageChunking = "passage_chunking"
	EvaluationPassageIDMetadataKey       = "evaluation_pid"
)

// EvaluationDatasetDescriptor identifies the exact dataset content used by a run.
type EvaluationDatasetDescriptor struct {
	ID                 string `json:"id"`
	Version            string `json:"version"`
	ContentFingerprint string `json:"content_fingerprint"`
	QueryCount         int    `json:"query_count"`
	CorpusCount        int    `json:"corpus_count"`
	CaseCount          int    `json:"case_count"`
	IngestionMode      string `json:"ingestion_mode"`
}

// EvaluationPassage is one corpus entry identified by its source passage ID.
type EvaluationPassage struct {
	PID  int
	Text string
}

// EvaluationDataset contains the complete corpus, validated cases and their immutable descriptor.
type EvaluationDataset struct {
	Descriptor EvaluationDatasetDescriptor
	Corpus     []EvaluationPassage
	Cases      []*QAPair
}

// EvaluationRunConfig records the effective, non-secret inputs used by one run.
type EvaluationRunConfig struct {
	SchemaVersion         string                      `json:"schema_version"`
	Dataset               EvaluationDatasetDescriptor `json:"dataset"`
	SourceKnowledgeBaseID string                      `json:"source_knowledge_base_id,omitempty"`
	Models                EvaluationModelConfigSet    `json:"models"`
	Chunking              EvaluationChunkingConfig    `json:"chunking"`
	Retrieval             EvaluationRetrievalConfig   `json:"retrieval"`
	Generation            EvaluationGenerationConfig  `json:"generation"`
	Indexing              EvaluationIndexingConfig    `json:"indexing"`
	Runtime               EvaluationRuntimeConfig     `json:"runtime"`
	ConfigHash            string                      `json:"config_hash"`
}

// String returns the JSON representation used by logs and contract tests.
func (c *EvaluationRunConfig) String() string {
	data, _ := json.Marshal(c)
	return string(data)
}

type EvaluationModelConfigSet struct {
	Embedding EvaluationModelConfig  `json:"embedding"`
	Chat      EvaluationModelConfig  `json:"chat"`
	Rerank    *EvaluationModelConfig `json:"rerank,omitempty"`
}

// EvaluationModelConfig excludes credentials and stores fingerprints for mutable settings.
type EvaluationModelConfig struct {
	ID                    string      `json:"id"`
	Name                  string      `json:"name"`
	DisplayName           string      `json:"display_name,omitempty"`
	Type                  ModelType   `json:"type"`
	Source                ModelSource `json:"source"`
	Provider              string      `json:"provider,omitempty"`
	InterfaceType         string      `json:"interface_type,omitempty"`
	EmbeddingDimension    int         `json:"embedding_dimension,omitempty"`
	MaxConcurrency        int         `json:"max_concurrency,omitempty"`
	EndpointFingerprint   string      `json:"endpoint_fingerprint,omitempty"`
	ParametersFingerprint string      `json:"parameters_fingerprint"`
	UpdatedAt             time.Time   `json:"updated_at"`
}

type EvaluationChunkingConfig struct {
	Applied    bool                         `json:"applied"`
	SourceUnit string                       `json:"source_unit"`
	Config     EvaluationChunkingParameters `json:"config"`
}

// EvaluationChunkingParameters contains only settings used by passage chunking.
type EvaluationChunkingParameters struct {
	ChunkSize         int      `json:"chunk_size"`
	ChunkOverlap      int      `json:"chunk_overlap"`
	Separators        []string `json:"separators"`
	EnableParentChild bool     `json:"enable_parent_child,omitempty"`
	ParentChunkSize   int      `json:"parent_chunk_size,omitempty"`
	ChildChunkSize    int      `json:"child_chunk_size,omitempty"`
	Strategy          string   `json:"strategy,omitempty"`
	TokenLimit        int      `json:"token_limit,omitempty"`
	Languages         []string `json:"languages,omitempty"`
}

type EvaluationRetrievalConfig struct {
	VectorThreshold  float64 `json:"vector_threshold"`
	KeywordThreshold float64 `json:"keyword_threshold"`
	EmbeddingTopK    int     `json:"embedding_top_k"`
	RerankTopK       int     `json:"rerank_top_k"`
	RerankThreshold  float64 `json:"rerank_threshold"`
}

type EvaluationGenerationConfig struct {
	MaxTokens                   int     `json:"max_tokens"`
	MaxCompletionTokens         int     `json:"max_completion_tokens"`
	Temperature                 float64 `json:"temperature"`
	TopP                        float64 `json:"top_p"`
	TopK                        int     `json:"top_k"`
	Seed                        int     `json:"seed"`
	RepeatPenalty               float64 `json:"repeat_penalty"`
	FrequencyPenalty            float64 `json:"frequency_penalty"`
	PresencePenalty             float64 `json:"presence_penalty"`
	Thinking                    *bool   `json:"thinking,omitempty"`
	PromptFingerprint           string  `json:"prompt_fingerprint"`
	ContextFingerprint          string  `json:"context_fingerprint"`
	NoMatchPrefixFingerprint    string  `json:"no_match_prefix_fingerprint"`
	FallbackResponseFingerprint string  `json:"fallback_response_fingerprint"`
	FallbackPromptFingerprint   string  `json:"fallback_prompt_fingerprint"`
}

type EvaluationIndexingConfig struct {
	VectorEnabled  bool   `json:"vector_enabled"`
	KeywordEnabled bool   `json:"keyword_enabled"`
	VectorStoreID  string `json:"vector_store_id,omitempty"`
}

type EvaluationRuntimeConfig struct {
	CaseConcurrency    int    `json:"case_concurrency"`
	MetricVersion      string `json:"metric_version"`
	ResultVersion      string `json:"result_version"`
	ApplicationVersion string `json:"application_version,omitempty"`
}
