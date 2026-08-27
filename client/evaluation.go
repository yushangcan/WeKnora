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
	"strconv"
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
	Reproducibility       EvaluationReproducibility   `json:"reproducibility"`
	ConfigHash            string                      `json:"config_hash"`
}

type EvaluationDatasetDescriptor struct {
	ID                 string                  `json:"id"`
	Version            string                  `json:"version"`
	ContentFingerprint string                  `json:"content_fingerprint"`
	Files              []EvaluationDatasetFile `json:"files,omitempty"`
	QueryCount         int                     `json:"query_count"`
	CorpusCount        int                     `json:"corpus_count"`
	CaseCount          int                     `json:"case_count"`
	IngestionMode      string                  `json:"ingestion_mode"`
}

type EvaluationDatasetFile struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	Size        int64  `json:"size"`
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
	CommitSHA          string `json:"commit_sha"`
	VCSModified        bool   `json:"vcs_modified"`
	CommitAvailable    bool   `json:"commit_available"`
}

type EvaluationReproducibility struct {
	Status   string              `json:"status"`
	Warnings []EvaluationWarning `json:"warnings"`
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
	CaseID      string                 `json:"case_id"`
	Status      string                 `json:"status"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at"`
	DurationMS  int64                  `json:"duration_ms"`
	Usage       EvaluationUsageResult  `json:"usage"`
	Evidence    EvaluationCaseEvidence `json:"evidence"`
	Warnings    []EvaluationWarning    `json:"warnings"`
}

type EvaluationCaseEvidence struct {
	QID                        int                `json:"qid"`
	QuestionFingerprint        string             `json:"question_fingerprint"`
	ReferenceAnswerFingerprint string             `json:"reference_answer_fingerprint"`
	GeneratedAnswerFingerprint string             `json:"generated_answer_fingerprint"`
	GroundTruthPIDs            []int              `json:"ground_truth_pids"`
	SearchPIDs                 []int              `json:"search_pids"`
	RerankPIDs                 []int              `json:"rerank_pids"`
	MetricInputPIDs            []int              `json:"metric_input_pids"`
	UnmappedResultCount        int                `json:"unmapped_result_count"`
	Metrics                    *EvaluationMetrics `json:"metrics,omitempty"`
	FailureStage               string             `json:"failure_stage,omitempty"`
}

type EvaluationWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// EvaluationRunStatus is the persisted lifecycle state of an evaluation run.
type EvaluationRunStatus string

const (
	EvaluationRunStatusPending EvaluationRunStatus = "pending"
	EvaluationRunStatusRunning EvaluationRunStatus = "running"
	EvaluationRunStatusSuccess EvaluationRunStatus = "success"
	EvaluationRunStatusPartial EvaluationRunStatus = "partial"
	EvaluationRunStatusFailed  EvaluationRunStatus = "failed"
)

// EvaluationRunListFilter selects one page of persisted evaluation runs.
type EvaluationRunListFilter struct {
	Status           EvaluationRunStatus
	DatasetID        string
	ConfigHash       string
	EmbeddingModelID string
	ChatModelID      string
	RerankModelID    string
	StartedFrom      *time.Time
	StartedTo        *time.Time
	Page             int
	PageSize         int
}

type EvaluationCaseStatusCounts struct {
	Total   int64 `json:"total"`
	Pending int64 `json:"pending"`
	Running int64 `json:"running"`
	Success int64 `json:"success"`
	Partial int64 `json:"partial"`
	Failed  int64 `json:"failed"`
}

type EvaluationRunProgress struct {
	Total    int                        `json:"total"`
	Finished int                        `json:"finished"`
	Cases    EvaluationCaseStatusCounts `json:"cases"`
}

// EvaluationRunSummary contains the persisted list projection of one run.
type EvaluationRunSummary struct {
	RunID                 string                      `json:"run_id"`
	Status                EvaluationRunStatus         `json:"status"`
	Dataset               EvaluationDatasetDescriptor `json:"dataset"`
	SourceKnowledgeBaseID string                      `json:"source_knowledge_base_id,omitempty"`
	ConfigHash            string                      `json:"config_hash"`
	ConfigSchemaVersion   string                      `json:"config_schema_version"`
	MetricVersion         string                      `json:"metric_version"`
	ResultVersion         string                      `json:"result_version"`
	Models                EvaluationModelConfigSet    `json:"models"`
	Reproducibility       EvaluationReproducibility   `json:"reproducibility"`
	Progress              EvaluationRunProgress       `json:"progress"`
	Retrieval             *EvaluationRetrievalResult  `json:"retrieval"`
	Answer                *EvaluationAnswerResult     `json:"answer"`
	Usage                 EvaluationUsageResult       `json:"usage"`
	Cost                  EvaluationCostResult        `json:"cost"`
	Timing                EvaluationTimingResult      `json:"timing"`
	Warnings              []EvaluationWarning         `json:"warnings"`
	ErrorMessage          string                      `json:"error_message,omitempty"`
	StartedAt             time.Time                   `json:"started_at"`
	CompletedAt           *time.Time                  `json:"completed_at"`
	CreatedAt             time.Time                   `json:"created_at"`
	UpdatedAt             time.Time                   `json:"updated_at"`
}

type EvaluationRunPage struct {
	Items    []EvaluationRunSummary `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
}

type EvaluationRunOverview struct {
	Summary EvaluationRunSummary `json:"summary"`
	Config  *EvaluationRunConfig `json:"config"`
	Metric  *EvaluationMetrics   `json:"metric,omitempty"`
}

type EvaluationCasePage struct {
	Items    []EvaluationCaseResult `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
}

type EvaluationComparisonCompatibility struct {
	Comparable bool     `json:"comparable"`
	Reasons    []string `json:"reasons"`
	Warnings   []string `json:"warnings"`
}

type EvaluationValueDelta struct {
	Baseline *float64 `json:"baseline"`
	Value    *float64 `json:"value"`
	Absolute *float64 `json:"absolute"`
	Percent  *float64 `json:"percent"`
}

type EvaluationQualityDeltas struct {
	Precision EvaluationValueDelta `json:"precision"`
	Recall    EvaluationValueDelta `json:"recall"`
	NDCG3     EvaluationValueDelta `json:"ndcg3"`
	NDCG10    EvaluationValueDelta `json:"ndcg10"`
	MRR       EvaluationValueDelta `json:"mrr"`
	MAP       EvaluationValueDelta `json:"map"`
	BLEU1     EvaluationValueDelta `json:"bleu1"`
	BLEU2     EvaluationValueDelta `json:"bleu2"`
	BLEU4     EvaluationValueDelta `json:"bleu4"`
	ROUGE1    EvaluationValueDelta `json:"rouge1"`
	ROUGE2    EvaluationValueDelta `json:"rouge2"`
	ROUGEL    EvaluationValueDelta `json:"rougel"`
}

type EvaluationCostDeltas struct {
	Amount           EvaluationValueDelta `json:"amount"`
	Calls            EvaluationValueDelta `json:"calls"`
	PromptTokens     EvaluationValueDelta `json:"prompt_tokens"`
	CompletionTokens EvaluationValueDelta `json:"completion_tokens"`
	TotalTokens      EvaluationValueDelta `json:"total_tokens"`
	CachedTokens     EvaluationValueDelta `json:"cached_tokens"`
}

type EvaluationTimingDeltas struct {
	TotalWallTimeMS       EvaluationValueDelta `json:"total_wall_time_ms"`
	PreparationMS         EvaluationValueDelta `json:"preparation_ms"`
	EvaluationMS          EvaluationValueDelta `json:"evaluation_ms"`
	CleanupMS             EvaluationValueDelta `json:"cleanup_ms"`
	CaseAverageMS         EvaluationValueDelta `json:"case_avg_ms"`
	CaseP50MS             EvaluationValueDelta `json:"case_p50_ms"`
	CaseP95MS             EvaluationValueDelta `json:"case_p95_ms"`
	ModelCallCumulativeMS EvaluationValueDelta `json:"model_call_cumulative_ms"`
}

type EvaluationRunComparison struct {
	Run                  EvaluationRunSummary              `json:"run"`
	Config               *EvaluationRunConfig              `json:"config"`
	QualityCompatibility EvaluationComparisonCompatibility `json:"quality_compatibility"`
	CostCompatibility    EvaluationComparisonCompatibility `json:"cost_compatibility"`
	TimingCompatibility  EvaluationComparisonCompatibility `json:"timing_compatibility"`
	Quality              EvaluationQualityDeltas           `json:"quality"`
	Cost                 EvaluationCostDeltas              `json:"cost"`
	Timing               EvaluationTimingDeltas            `json:"timing"`
}

type EvaluationComparison struct {
	BaselineID string                    `json:"baseline_id"`
	Runs       []EvaluationRunComparison `json:"runs"`
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

// ListEvaluationRuns returns one filtered page of persisted evaluation runs.
func (c *Client) ListEvaluationRuns(
	ctx context.Context,
	filter EvaluationRunListFilter,
) (*EvaluationRunPage, error) {
	query := url.Values{}
	if filter.Status != "" {
		query.Set("status", string(filter.Status))
	}
	if filter.DatasetID != "" {
		query.Set("dataset_id", filter.DatasetID)
	}
	if filter.ConfigHash != "" {
		query.Set("config_hash", filter.ConfigHash)
	}
	if filter.EmbeddingModelID != "" {
		query.Set("embedding_model_id", filter.EmbeddingModelID)
	}
	if filter.ChatModelID != "" {
		query.Set("chat_model_id", filter.ChatModelID)
	}
	if filter.RerankModelID != "" {
		query.Set("rerank_model_id", filter.RerankModelID)
	}
	if filter.StartedFrom != nil {
		query.Set("started_from", filter.StartedFrom.Format(time.RFC3339))
	}
	if filter.StartedTo != nil {
		query.Set("started_to", filter.StartedTo.Format(time.RFC3339))
	}
	if filter.Page > 0 {
		query.Set("page", strconv.Itoa(filter.Page))
	}
	if filter.PageSize > 0 {
		query.Set("page_size", strconv.Itoa(filter.PageSize))
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/evaluation/runs", nil, query)
	if err != nil {
		return nil, err
	}
	var response struct {
		Success bool              `json:"success"`
		Data    EvaluationRunPage `json:"data"`
	}
	if err := parseResponse(resp, &response); err != nil {
		return nil, err
	}
	return &response.Data, nil
}

// GetEvaluationRun returns one persisted run without loading its case rows.
func (c *Client) GetEvaluationRun(ctx context.Context, runID string) (*EvaluationRunOverview, error) {
	path := fmt.Sprintf("/api/v1/evaluation/runs/%s", url.PathEscape(runID))
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Success bool                  `json:"success"`
		Data    EvaluationRunOverview `json:"data"`
	}
	if err := parseResponse(resp, &response); err != nil {
		return nil, err
	}
	return &response.Data, nil
}

// ListEvaluationRunCases returns an independently paged case evidence list.
func (c *Client) ListEvaluationRunCases(
	ctx context.Context,
	runID string,
	status EvaluationRunStatus,
	page int,
	pageSize int,
) (*EvaluationCasePage, error) {
	query := url.Values{}
	if status != "" {
		query.Set("status", string(status))
	}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		query.Set("page_size", strconv.Itoa(pageSize))
	}
	path := fmt.Sprintf("/api/v1/evaluation/runs/%s/cases", url.PathEscape(runID))
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil, query)
	if err != nil {
		return nil, err
	}
	var response struct {
		Success bool               `json:"success"`
		Data    EvaluationCasePage `json:"data"`
	}
	if err := parseResponse(resp, &response); err != nil {
		return nil, err
	}
	return &response.Data, nil
}

// CompareEvaluationRuns returns persisted baseline-relative differences.
func (c *Client) CompareEvaluationRuns(
	ctx context.Context,
	baselineID string,
	runIDs []string,
) (*EvaluationComparison, error) {
	query := url.Values{}
	query.Set("baseline_id", baselineID)
	for _, runID := range runIDs {
		query.Add("run_ids", runID)
	}
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/evaluation/comparison", nil, query)
	if err != nil {
		return nil, err
	}
	var response struct {
		Success bool                 `json:"success"`
		Data    EvaluationComparison `json:"data"`
	}
	if err := parseResponse(resp, &response); err != nil {
		return nil, err
	}
	return &response.Data, nil
}
