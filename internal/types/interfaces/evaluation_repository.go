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
	// MarkAllInterruptedRunsFailed closes expired/unowned non-terminal rows
	// across all tenants. Active execution leases must be preserved.
	MarkAllInterruptedRunsFailed(
		ctx context.Context,
		completedAt time.Time,
		errorMessage string,
	) (int64, error)
}

// EvaluationLeaseRepository is implemented by repositories that support
// multi-instance ownership of an in-flight evaluation. It is intentionally a
// separate optional interface so existing embedders and test doubles remain
// source-compatible.
type EvaluationLeaseRepository interface {
	RenewEvaluationRunLease(ctx context.Context, tenantID uint64, runID, ownerID string, leaseUntil time.Time) (bool, error)
	ReleaseEvaluationRunLease(ctx context.Context, tenantID uint64, runID, ownerID string) error
	RecoverExpiredEvaluationRuns(ctx context.Context, now time.Time, errorMessage string) (int64, error)
}
