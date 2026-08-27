package types

import "time"

// EvaluationRunListFilter contains tenant-scoped history filters.
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

// EvaluationCaseStatusCounts summarizes persisted case rows without loading them.
type EvaluationCaseStatusCounts struct {
	Total   int64 `json:"total"`
	Pending int64 `json:"pending"`
	Running int64 `json:"running"`
	Success int64 `json:"success"`
	Partial int64 `json:"partial"`
	Failed  int64 `json:"failed"`
}

// EvaluationRunProgress combines task progress with persisted case counts.
type EvaluationRunProgress struct {
	Total    int                        `json:"total"`
	Finished int                        `json:"finished"`
	Cases    EvaluationCaseStatusCounts `json:"cases"`
}

// EvaluationRunSummary is the lightweight projection used by history lists.
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

// EvaluationRunPage is one stable, tenant-scoped page of evaluation runs.
type EvaluationRunPage struct {
	Items    []EvaluationRunSummary `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
}

// EvaluationRunOverview contains one run without its potentially large case list.
type EvaluationRunOverview struct {
	Summary EvaluationRunSummary `json:"summary"`
	Config  *EvaluationRunConfig `json:"config"`
	Metric  *MetricResult        `json:"metric,omitempty"`
}

// EvaluationCasePage is a separately paged collection of case audit evidence.
type EvaluationCasePage struct {
	Items    []EvaluationCaseResult `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
}

// EvaluationComparisonCompatibility explains whether a dimension can be compared.
type EvaluationComparisonCompatibility struct {
	Comparable bool     `json:"comparable"`
	Reasons    []string `json:"reasons"`
	Warnings   []string `json:"warnings"`
}

// EvaluationValueDelta uses candidate minus baseline for every dimension.
type EvaluationValueDelta struct {
	Baseline *float64 `json:"baseline"`
	Value    *float64 `json:"value"`
	Absolute *float64 `json:"absolute"`
	Percent  *float64 `json:"percent"`
}

// EvaluationQualityDeltas contains all persisted retrieval and answer metrics.
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

// EvaluationCostDeltas contains monetary and usage changes for one candidate.
type EvaluationCostDeltas struct {
	Amount           EvaluationValueDelta `json:"amount"`
	Calls            EvaluationValueDelta `json:"calls"`
	PromptTokens     EvaluationValueDelta `json:"prompt_tokens"`
	CompletionTokens EvaluationValueDelta `json:"completion_tokens"`
	TotalTokens      EvaluationValueDelta `json:"total_tokens"`
	CachedTokens     EvaluationValueDelta `json:"cached_tokens"`
}

// EvaluationTimingDeltas contains persisted wall-clock and case latency changes.
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

// EvaluationRunComparison is one baseline-relative comparison row.
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

// EvaluationComparison contains an ordered, baseline-relative comparison.
type EvaluationComparison struct {
	BaselineID string                    `json:"baseline_id"`
	Runs       []EvaluationRunComparison `json:"runs"`
}
