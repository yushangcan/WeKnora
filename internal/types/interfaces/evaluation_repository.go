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
	UpdateRun(ctx context.Context, detail *types.EvaluationDetail) error
	SaveCaseProgress(
		ctx context.Context,
		detail *types.EvaluationDetail,
		caseResult *types.EvaluationCaseResult,
	) error
	SaveTerminalRun(ctx context.Context, detail *types.EvaluationDetail) error
	MarkInterruptedRunsFailed(ctx context.Context, completedAt time.Time, errorMessage string) (int64, error)
}
