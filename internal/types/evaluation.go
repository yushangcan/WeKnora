package types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/yanyiwu/gojieba"
)

// Jieba is a global instance of Chinese text segmentation tool
var Jieba *gojieba.Jieba = newJieba()

func newJieba() *gojieba.Jieba {
	dictDir := os.Getenv("JIEBA_DICT_DIR")
	if dictDir == "" {
		return gojieba.NewJieba()
	}

	return gojieba.NewJieba(
		filepath.Join(dictDir, "jieba.dict.utf8"),
		filepath.Join(dictDir, "hmm_model.utf8"),
		filepath.Join(dictDir, "user.dict.utf8"),
		filepath.Join(dictDir, "idf.utf8"),
		filepath.Join(dictDir, "stop_words.utf8"),
	)
}

// EvaluationStatue represents the status of an evaluation task
type EvaluationStatue int

const (
	EvaluationStatuePending EvaluationStatue = iota // Task is waiting to start
	EvaluationStatueRunning                         // Task is in progress
	EvaluationStatueSuccess                         // Task completed successfully
	EvaluationStatueFailed                          // Task failed
)

// EvaluationTask contains information about an evaluation task
type EvaluationTask struct {
	ID        string `json:"id"`         // Unique task ID
	TenantID  uint64 `json:"tenant_id"`  // Tenant/Organization ID
	DatasetID string `json:"dataset_id"` // Dataset ID for evaluation

	StartTime time.Time        `json:"start_time"`        // Task start time
	Status    EvaluationStatue `json:"status"`            // Current task status
	ErrMsg    string           `json:"err_msg,omitempty"` // Error message if failed

	Total    int `json:"total,omitempty"`    // Total items to evaluate
	Finished int `json:"finished,omitempty"` // Completed items count
}

// EvaluationDetail contains detailed evaluation information
type EvaluationDetail struct {
	Task   *EvaluationTask      `json:"task"`             // Evaluation task info
	Params *ChatManage          `json:"params"`           // Evaluation parameters
	Config *EvaluationRunConfig `json:"config,omitempty"` // Effective reproducibility configuration
	Metric *MetricResult        `json:"metric,omitempty"` // Evaluation metrics
	// Result is the stage-one, in-memory four-dimension observation result.
	// Metric remains available for backwards compatibility.
	Result *EvaluationRunResult `json:"result,omitempty"`
}

// Evaluation result schema and status constants are strings because they are
// part of the public JSON contract. They are deliberately separate from the
// legacy integer EvaluationStatue used by EvaluationTask.
const EvaluationResultSchemaVersion = "evaluation-run/v1"

type EvaluationRunStatus string

const (
	EvaluationRunStatusPending EvaluationRunStatus = "pending"
	EvaluationRunStatusRunning EvaluationRunStatus = "running"
	EvaluationRunStatusSuccess EvaluationRunStatus = "success"
	EvaluationRunStatusPartial EvaluationRunStatus = "partial"
	EvaluationRunStatusFailed  EvaluationRunStatus = "failed"
)

type EvaluationPhase string

const (
	EvaluationPhasePreparation EvaluationPhase = "preparation"
	EvaluationPhaseEvaluation  EvaluationPhase = "evaluation"
	EvaluationPhaseCleanup     EvaluationPhase = "cleanup"
)

type EvaluationUsageSource string

const (
	EvaluationUsageSourceProviderReported EvaluationUsageSource = "provider_reported"
	EvaluationUsageSourceUnavailable      EvaluationUsageSource = "unavailable"
)

type EvaluationUsageStatus string

const (
	EvaluationUsageStatusComplete      EvaluationUsageStatus = "complete"
	EvaluationUsageStatusPartial       EvaluationUsageStatus = "partial"
	EvaluationUsageStatusUnavailable   EvaluationUsageStatus = "unavailable"
	EvaluationUsageStatusNotApplicable EvaluationUsageStatus = "not_applicable"
)

type EvaluationCostStatus string

const (
	EvaluationCostStatusComplete      EvaluationCostStatus = "complete"
	EvaluationCostStatusPartial       EvaluationCostStatus = "partial"
	EvaluationCostStatusUnavailable   EvaluationCostStatus = "unavailable"
	EvaluationCostStatusNotApplicable EvaluationCostStatus = "not_applicable"
)

type EvaluationModelType string

const (
	EvaluationModelTypeChat      EvaluationModelType = "chat"
	EvaluationModelTypeEmbedding EvaluationModelType = "embedding"
	EvaluationModelTypeRerank    EvaluationModelType = "rerank"
)

type EvaluationModelOperation string

const (
	EvaluationOperationChat       EvaluationModelOperation = "chat"
	EvaluationOperationEmbed      EvaluationModelOperation = "embed"
	EvaluationOperationBatchEmbed EvaluationModelOperation = "batch_embed"
	EvaluationOperationRerank     EvaluationModelOperation = "rerank"
)

// EvaluationRunResult combines the existing quality metrics with application-
// side model usage and timing observations. Persistence stores this response
// contract as a versioned snapshot rather than recalculating historical runs.
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
	RunID          string              `json:"run_id"`
	TenantID       uint64              `json:"tenant_id"`
	DatasetID      string              `json:"dataset_id"`
	StartedAt      time.Time           `json:"started_at"`
	CompletedAt    *time.Time          `json:"completed_at"`
	Status         EvaluationRunStatus `json:"status"`
	PricingVersion string              `json:"pricing_version,omitempty"`
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
	Status               EvaluationUsageStatus  `json:"status"`
	Calls                EvaluationCallCounts   `json:"calls"`
	Tokens               EvaluationTokenTotals  `json:"tokens"`
	CacheStatus          string                 `json:"cache_status"`
	ReportedCallCount    int                    `json:"reported_call_count"`
	UnavailableCallCount int                    `json:"unavailable_call_count"`
	ByModel              []EvaluationModelUsage `json:"by_model"`
	ByPhase              []EvaluationPhaseUsage `json:"by_phase"`
}

type EvaluationModelUsage struct {
	ModelType   EvaluationModelType      `json:"model_type"`
	ModelID     string                   `json:"model_id"`
	ModelName   string                   `json:"model_name"`
	Operation   EvaluationModelOperation `json:"operation"`
	UsageSource EvaluationUsageSource    `json:"usage_source"`
	Calls       EvaluationCallCounts     `json:"calls"`
	Tokens      EvaluationTokenTotals    `json:"tokens"`
	DurationMS  int64                    `json:"duration_ms"`
}

type EvaluationPhaseUsage struct {
	Phase       EvaluationPhase       `json:"phase"`
	UsageSource EvaluationUsageSource `json:"usage_source"`
	Calls       EvaluationCallCounts  `json:"calls"`
	Tokens      EvaluationTokenTotals `json:"tokens"`
	DurationMS  int64                 `json:"duration_ms"`
}

type EvaluationCostResult struct {
	Status         EvaluationCostStatus `json:"status"`
	Source         string               `json:"source"`
	Currency       string               `json:"currency,omitempty"`
	Amount         *float64             `json:"amount"`
	PricingVersion string               `json:"pricing_version,omitempty"`
	Warnings       []EvaluationWarning  `json:"warnings"`
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
	Status      EvaluationRunStatus    `json:"status"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt *time.Time             `json:"completed_at"`
	DurationMS  int64                  `json:"duration_ms"`
	Usage       EvaluationUsageResult  `json:"usage"`
	Evidence    EvaluationCaseEvidence `json:"evidence"`
	Warnings    []EvaluationWarning    `json:"warnings"`
}

// EvaluationCaseEvidence retains non-text inputs and outputs needed to audit one case score.
type EvaluationCaseEvidence struct {
	QID                        int           `json:"qid"`
	QuestionFingerprint        string        `json:"question_fingerprint"`
	ReferenceAnswerFingerprint string        `json:"reference_answer_fingerprint"`
	GeneratedAnswerFingerprint string        `json:"generated_answer_fingerprint"`
	GroundTruthPIDs            []int         `json:"ground_truth_pids"`
	SearchPIDs                 []int         `json:"search_pids"`
	RerankPIDs                 []int         `json:"rerank_pids"`
	MetricInputPIDs            []int         `json:"metric_input_pids"`
	UnmappedResultCount        int           `json:"unmapped_result_count"`
	Metrics                    *MetricResult `json:"metrics,omitempty"`
	FailureStage               string        `json:"failure_stage,omitempty"`
}

type EvaluationWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// String returns JSON representation of EvaluationTask
func (e *EvaluationTask) String() string {
	b, _ := json.Marshal(e)
	return string(b)
}

// MetricInput contains input data for metric calculation
type MetricInput struct {
	RetrievalGT  [][]int // Ground truth for retrieval
	RetrievalIDs []int   // Retrieved IDs

	GeneratedTexts string // Generated text for evaluation
	GeneratedGT    string // Ground truth text for comparison
}

// MetricResult contains evaluation metrics
type MetricResult struct {
	RetrievalMetrics  RetrievalMetrics  `json:"retrieval_metrics"`  // Retrieval performance metrics
	GenerationMetrics GenerationMetrics `json:"generation_metrics"` // Text generation quality metrics
}

// RetrievalMetrics contains metrics for retrieval evaluation
type RetrievalMetrics struct {
	Precision float64 `json:"precision"` // Precision score
	Recall    float64 `json:"recall"`    // Recall score

	NDCG3  float64 `json:"ndcg3"`  // Normalized Discounted Cumulative Gain at 3
	NDCG10 float64 `json:"ndcg10"` // Normalized Discounted Cumulative Gain at 10
	MRR    float64 `json:"mrr"`    // Mean Reciprocal Rank
	MAP    float64 `json:"map"`    // Mean Average Precision
}

// GenerationMetrics contains metrics for text generation evaluation
type GenerationMetrics struct {
	BLEU1 float64 `json:"bleu1"` // BLEU-1 score
	BLEU2 float64 `json:"bleu2"` // BLEU-2 score
	BLEU4 float64 `json:"bleu4"` // BLEU-4 score

	ROUGE1 float64 `json:"rouge1"` // ROUGE-1 score
	ROUGE2 float64 `json:"rouge2"` // ROUGE-2 score
	ROUGEL float64 `json:"rougel"` // ROUGE-L score
}

// EvalState represents different stages of evaluation process
type EvalState int

const (
	StateBegin             EvalState = iota // Evaluation started
	StateAfterQaPairs                       // After loading QA pairs
	StateAfterDataset                       // After processing dataset
	StateAfterEmbedding                     // After generating embeddings
	StateAfterVectorSearch                  // After vector search
	StateAfterRerank                        // After reranking
	StateAfterComplete                      // After completion
	StateEnd                                // Evaluation ended
)
