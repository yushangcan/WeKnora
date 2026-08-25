// Package client provides the implementation for interacting with the WeKnora API
// The Evaluation related interfaces are used for starting and retrieving model evaluation task results
// Evaluation tasks can be used to measure model performance and
// compare different embedding models, chat models, and reranking models
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// EvaluationStatus mirrors the server's legacy integer task status.
type EvaluationStatus int

const (
	EvaluationStatusPending EvaluationStatus = iota
	EvaluationStatusRunning
	EvaluationStatusSuccess
	EvaluationStatusFailed
)

// EvaluationTask represents the server's task object.
type EvaluationTask struct {
	ID         string           `json:"id"`
	TenantID   uint64           `json:"tenant_id"`
	DatasetID  string           `json:"dataset_id"`
	StartTime  time.Time        `json:"start_time"`
	Status     string           `json:"-"`
	StatusCode EvaluationStatus `json:"-"`
	ErrorMsg   string           `json:"err_msg,omitempty"`
	Total      int              `json:"total,omitempty"`
	Finished   int              `json:"finished,omitempty"`

	// Deprecated fields are retained for source compatibility with the
	// earlier client DTO. The current server does not populate them here.
	Progress    int    `json:"progress,omitempty"`
	EmbeddingID string `json:"embedding_id,omitempty"`
	ChatID      string `json:"chat_id,omitempty"`
	RerankID    string `json:"rerank_id,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	CompleteAt  string `json:"complete_at,omitempty"`
}

func (task *EvaluationTask) UnmarshalJSON(data []byte) error {
	type taskAlias EvaluationTask
	var wire struct {
		*taskAlias
		Status      json.RawMessage `json:"status"`
		LegacyError string          `json:"error_msg"`
	}
	wire.taskAlias = (*taskAlias)(task)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if task.ErrorMsg == "" {
		task.ErrorMsg = wire.LegacyError
	}
	if len(wire.Status) == 0 || string(wire.Status) == "null" {
		return nil
	}
	var statusCode EvaluationStatus
	if err := json.Unmarshal(wire.Status, &statusCode); err == nil {
		task.StatusCode = statusCode
		task.Status = evaluationStatusName(statusCode)
		return nil
	}
	if err := json.Unmarshal(wire.Status, &task.Status); err != nil {
		return fmt.Errorf("decode evaluation task status: %w", err)
	}
	task.StatusCode = evaluationStatusCode(task.Status)
	return nil
}

func evaluationStatusName(status EvaluationStatus) string {
	switch status {
	case EvaluationStatusPending:
		return "pending"
	case EvaluationStatusRunning:
		return "running"
	case EvaluationStatusSuccess:
		return "success"
	case EvaluationStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

func evaluationStatusCode(status string) EvaluationStatus {
	switch status {
	case "running":
		return EvaluationStatusRunning
	case "success", "completed":
		return EvaluationStatusSuccess
	case "failed":
		return EvaluationStatusFailed
	default:
		return EvaluationStatusPending
	}
}

// EvaluationResult represents the actual server EvaluationDetail response.
type EvaluationResult struct {
	Task   *EvaluationTask      `json:"task"`
	Params json.RawMessage      `json:"params"`
	Config *EvaluationRunConfig `json:"config,omitempty"`
	Metric *EvaluationMetrics   `json:"metric,omitempty"`
	Result *EvaluationRunResult `json:"result,omitempty"`

	// Deprecated flat fields are retained so existing client code continues
	// to compile while callers migrate to Task, Metric and Result.
	TaskID       string                   `json:"task_id,omitempty"`
	Status       string                   `json:"status,omitempty"`
	Progress     int                      `json:"progress,omitempty"`
	TotalQueries int                      `json:"total_queries,omitempty"`
	TotalSamples int                      `json:"total_samples,omitempty"`
	Metrics      map[string]float64       `json:"metrics,omitempty"`
	QueriesStat  []map[string]interface{} `json:"queries_stat,omitempty"`
	CreatedAt    string                   `json:"created_at,omitempty"`
	CompleteAt   string                   `json:"complete_at,omitempty"`
	ErrorMsg     string                   `json:"error_msg,omitempty"`
}

// EvaluationRunConfig identifies the immutable inputs used by one run.
// Mutable or sensitive model settings are represented by fingerprints.
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

type EvaluationDatasetDescriptor struct {
	ID                 string `json:"id"`
	Version            string `json:"version"`
	ContentFingerprint string `json:"content_fingerprint"`
	QueryCount         int    `json:"query_count"`
	CorpusCount        int    `json:"corpus_count"`
	CaseCount          int    `json:"case_count"`
	IngestionMode      string `json:"ingestion_mode"`
}

type EvaluationModelConfigSet struct {
	Embedding EvaluationModelConfig  `json:"embedding"`
	Chat      EvaluationModelConfig  `json:"chat"`
	Rerank    *EvaluationModelConfig `json:"rerank,omitempty"`
}

type EvaluationModelConfig struct {
	ID                    string    `json:"id"`
	Name                  string    `json:"name"`
	DisplayName           string    `json:"display_name,omitempty"`
	Type                  string    `json:"type"`
	Source                string    `json:"source"`
	Provider              string    `json:"provider,omitempty"`
	InterfaceType         string    `json:"interface_type,omitempty"`
	EmbeddingDimension    int       `json:"embedding_dimension,omitempty"`
	MaxConcurrency        int       `json:"max_concurrency,omitempty"`
	EndpointFingerprint   string    `json:"endpoint_fingerprint,omitempty"`
	ParametersFingerprint string    `json:"parameters_fingerprint"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type EvaluationChunkingConfig struct {
	Applied    bool            `json:"applied"`
	SourceUnit string          `json:"source_unit"`
	Config     json.RawMessage `json:"config"`
}

type EvaluationRetrievalConfig struct {
	VectorThreshold  float64 `json:"vector_threshold"`
	KeywordThreshold float64 `json:"keyword_threshold"`
	EmbeddingTopK    int     `json:"embedding_top_k"`
	RerankTopK       int     `json:"rerank_top_k"`
	RerankThreshold  float64 `json:"rerank_threshold"`
}

type EvaluationGenerationConfig struct {
	MaxTokens           int     `json:"max_tokens"`
	MaxCompletionTokens int     `json:"max_completion_tokens"`
	Temperature         float64 `json:"temperature"`
	TopP                float64 `json:"top_p"`
	TopK                int     `json:"top_k"`
	Seed                int     `json:"seed"`
	PromptFingerprint   string  `json:"prompt_fingerprint"`
	ContextFingerprint  string  `json:"context_fingerprint"`
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

type EvaluationMetrics struct {
	Retrieval  EvaluationRetrievalResult `json:"retrieval_metrics"`
	Generation EvaluationAnswerResult    `json:"generation_metrics"`
}

type EvaluationRunResult struct {
	SchemaVersion string                     `json:"schema_version"`
	Run           EvaluationRunMetadata      `json:"run"`
	Retrieval     *EvaluationRetrievalResult `json:"retrieval"`
	Answer        *EvaluationAnswerResult    `json:"answer"`
	Usage         EvaluationUsageResult      `json:"usage"`
	Cost          EvaluationCostResult       `json:"cost"`
	Timing        EvaluationTimingResult     `json:"timing"`
	Cases         []EvaluationCaseResult     `json:"cases"`
	Warnings      []EvaluationWarning        `json:"warnings"`
}

type EvaluationRunMetadata struct {
	RunID          string     `json:"run_id"`
	TenantID       uint64     `json:"tenant_id"`
	DatasetID      string     `json:"dataset_id"`
	StartedAt      time.Time  `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	Status         string     `json:"status"`
	PricingVersion string     `json:"pricing_version,omitempty"`
}

type EvaluationRetrievalResult struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	NDCG3     float64 `json:"ndcg3"`
	NDCG10    float64 `json:"ndcg10"`
	MRR       float64 `json:"mrr"`
	MAP       float64 `json:"map"`
}

type EvaluationAnswerResult struct {
	BLEU1  float64 `json:"bleu1"`
	BLEU2  float64 `json:"bleu2"`
	BLEU4  float64 `json:"bleu4"`
	ROUGE1 float64 `json:"rouge1"`
	ROUGE2 float64 `json:"rouge2"`
	ROUGEL float64 `json:"rougel"`
}

type EvaluationCallCounts struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Items     int `json:"items"`
}

type EvaluationTokenTotals struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
	CacheMissTokens  int `json:"cache_miss_tokens"`
}

type EvaluationUsageResult struct {
	Status               string                 `json:"status"`
	Calls                EvaluationCallCounts   `json:"calls"`
	Tokens               EvaluationTokenTotals  `json:"tokens"`
	CacheStatus          string                 `json:"cache_status"`
	ReportedCallCount    int                    `json:"reported_call_count"`
	UnavailableCallCount int                    `json:"unavailable_call_count"`
	ByModel              []EvaluationModelUsage `json:"by_model"`
	ByPhase              []EvaluationPhaseUsage `json:"by_phase"`
}

type EvaluationModelUsage struct {
	ModelType   string                `json:"model_type"`
	ModelID     string                `json:"model_id"`
	ModelName   string                `json:"model_name"`
	Operation   string                `json:"operation"`
	UsageSource string                `json:"usage_source"`
	Calls       EvaluationCallCounts  `json:"calls"`
	Tokens      EvaluationTokenTotals `json:"tokens"`
	DurationMS  int64                 `json:"duration_ms"`
}

type EvaluationPhaseUsage struct {
	Phase       string                `json:"phase"`
	UsageSource string                `json:"usage_source"`
	Calls       EvaluationCallCounts  `json:"calls"`
	Tokens      EvaluationTokenTotals `json:"tokens"`
	DurationMS  int64                 `json:"duration_ms"`
}

type EvaluationCostResult struct {
	Status         string              `json:"status"`
	Source         string              `json:"source"`
	Currency       string              `json:"currency,omitempty"`
	Amount         *float64            `json:"amount"`
	PricingVersion string              `json:"pricing_version,omitempty"`
	Warnings       []EvaluationWarning `json:"warnings"`
}

type EvaluationTimingResult struct {
	TotalWallTimeMS       int64 `json:"total_wall_time_ms"`
	PreparationMS         int64 `json:"preparation_ms"`
	EvaluationMS          int64 `json:"evaluation_ms"`
	CleanupMS             int64 `json:"cleanup_ms"`
	CaseCount             int   `json:"case_count"`
	CaseAverageMS         int64 `json:"case_avg_ms"`
	CaseMinimumMS         int64 `json:"case_min_ms"`
	CaseMaximumMS         int64 `json:"case_max_ms"`
	CaseP50MS             int64 `json:"case_p50_ms"`
	CaseP95MS             int64 `json:"case_p95_ms"`
	ModelCallCumulativeMS int64 `json:"model_call_cumulative_ms"`
}

type EvaluationCaseResult struct {
	CaseID      string                `json:"case_id"`
	Status      string                `json:"status"`
	StartedAt   time.Time             `json:"started_at"`
	CompletedAt *time.Time            `json:"completed_at"`
	DurationMS  int64                 `json:"duration_ms"`
	Usage       EvaluationUsageResult `json:"usage"`
	Warnings    []EvaluationWarning   `json:"warnings"`
}

type EvaluationWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// EvaluationRequest represents an evaluation request
// Parameters used to start a new evaluation task
type EvaluationRequest struct {
	DatasetID       string `json:"dataset_id"`
	KnowledgeBaseID string `json:"knowledge_base_id,omitempty"`
	ChatModelID     string `json:"chat_id"`
	RerankModelID   string `json:"rerank_id,omitempty"`
	// Deprecated: the server selects the embedding model from the knowledge
	// base. This field is retained only for source compatibility.
	EmbeddingModelID string `json:"embedding_id,omitempty"`
}

// EvaluationTaskResponse represents an evaluation task response
// API response structure for evaluation tasks
type EvaluationTaskResponse struct {
	Success bool           `json:"success"`
	Data    EvaluationTask `json:"data"`
}

// UnmarshalJSON accepts both the server's current EvaluationDetail envelope
// and the earlier flat task shape while preserving the public Data field type.
func (response *EvaluationTaskResponse) UnmarshalJSON(data []byte) error {
	var wire struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	response.Success = wire.Success

	var detail EvaluationResult
	if err := json.Unmarshal(wire.Data, &detail); err != nil {
		return err
	}
	if detail.Task != nil {
		response.Data = *detail.Task
		return nil
	}
	return json.Unmarshal(wire.Data, &response.Data)
}

// EvaluationResultResponse represents an evaluation result response
// API response structure for evaluation results
type EvaluationResultResponse struct {
	Success bool             `json:"success"` // Whether operation was successful
	Data    EvaluationResult `json:"data"`    // Evaluation result data
}

// StartEvaluation starts an evaluation task
// Creates and starts a new evaluation task based on provided parameters
// Parameters:
//   - ctx: Context, used for passing request context information such as deadline, cancellation signals, etc.
//   - request: Evaluation request parameters, including dataset ID and model IDs
//
// Returns:
//   - *EvaluationTask: Created evaluation task information
//   - error: Error information if the request fails
func (c *Client) StartEvaluation(ctx context.Context, request *EvaluationRequest) (*EvaluationTask, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/evaluation", request, nil)
	if err != nil {
		return nil, err
	}

	var response EvaluationTaskResponse
	if err := parseResponse(resp, &response); err != nil {
		return nil, err
	}

	if response.Data.ID == "" {
		return nil, fmt.Errorf("evaluation response did not contain task")
	}
	return &response.Data, nil
}

// GetEvaluationResult retrieves evaluation results
// Retrieves detailed results for an evaluation task by task ID
// Parameters:
//   - ctx: Context, used for passing request context information
//   - taskID: Evaluation task ID, used to identify the specific evaluation task to query
//
// Returns:
//   - *EvaluationResult: Detailed evaluation task results
//   - error: Error information if the request fails
func (c *Client) GetEvaluationResult(ctx context.Context, taskID string) (*EvaluationResult, error) {
	queryParams := url.Values{}
	queryParams.Add("task_id", taskID)

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/evaluation", nil, queryParams)
	if err != nil {
		return nil, err
	}

	var response EvaluationResultResponse
	if err := parseResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response.Data, nil
}
