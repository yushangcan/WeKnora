package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// EvaluationService defines operations for evaluation tasks
type EvaluationService interface {
	// Evaluation starts a new evaluation task
	Evaluation(ctx context.Context, datasetID string, knowledgeBaseID string,
		chatModelID string, rerankModelID string,
	) (*types.EvaluationDetail, error)
	// EvaluationResult retrieves evaluation result by task ID
	EvaluationResult(ctx context.Context, taskID string) (*types.EvaluationDetail, error)
	// ListEvaluationRuns returns one filtered history page for the current tenant.
	ListEvaluationRuns(ctx context.Context, filter types.EvaluationRunListFilter) (*types.EvaluationRunPage, error)
	// GetEvaluationRun returns one lightweight run overview without all cases.
	GetEvaluationRun(ctx context.Context, runID string) (*types.EvaluationRunOverview, error)
	// ListEvaluationRunCases returns one page of case audit evidence.
	ListEvaluationRunCases(
		ctx context.Context,
		runID string,
		status types.EvaluationRunStatus,
		page int,
		pageSize int,
	) (*types.EvaluationCasePage, error)
	// CompareEvaluationRuns compares persisted snapshots without rerunning metrics.
	CompareEvaluationRuns(
		ctx context.Context,
		baselineID string,
		runIDs []string,
	) (*types.EvaluationComparison, error)
}

// Metrics defines interface for computing evaluation metrics
type Metrics interface {
	// Compute calculates metric score based on input data
	Compute(metricInput *types.MetricInput) float64
}

// EvalHook defines interface for evaluation process hooks
type EvalHook interface {
	// Handle processes evaluation state change
	Handle(ctx context.Context, state types.EvalState, index int, data interface{}) error
}

// DatasetService defines operations for dataset management
type DatasetService interface {
	// LoadDataset returns validated cases together with a stable content descriptor.
	LoadDataset(ctx context.Context, datasetID string) (*types.EvaluationDataset, error)
	// GetDatasetByID retrieves QA pairs from dataset by ID
	GetDatasetByID(ctx context.Context, datasetID string) ([]*types.QAPair, error)
}
