package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// EvaluationRepository persists run snapshots independently of source resources.
type EvaluationRepository interface {
	CreateRun(
		ctx context.Context,
		detail *types.EvaluationDetail,
		temporaryKnowledgeBaseID string,
	) error
	GetRun(ctx context.Context, tenantID uint64, runID string) (*types.EvaluationDetail, error)
	ListRuns(
		ctx context.Context,
		tenantID uint64,
		filter types.EvaluationRunListFilter,
	) (*types.EvaluationRunPage, error)
	GetRunOverview(ctx context.Context, tenantID uint64, runID string) (*types.EvaluationRunOverview, error)
	GetRunOverviews(
		ctx context.Context,
		tenantID uint64,
		runIDs []string,
	) ([]types.EvaluationRunOverview, error)
	ListRunCases(
		ctx context.Context,
		tenantID uint64,
		runID string,
		status types.EvaluationRunStatus,
		page int,
		pageSize int,
	) (*types.EvaluationCasePage, error)
	UpdateRun(ctx context.Context, detail *types.EvaluationDetail) error
	SaveCaseProgress(
		ctx context.Context,
		detail *types.EvaluationDetail,
		caseResult *types.EvaluationCaseResult,
	) error
	SaveTerminalRun(ctx context.Context, detail *types.EvaluationDetail) error
	MarkInterruptedRunsFailed(
		ctx context.Context,
		tenantID uint64,
		completedAt time.Time,
		errorMessage string,
	) (int64, error)
}
