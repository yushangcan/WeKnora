package container

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"gorm.io/gorm"
)

const evaluationRestartInterruptedMessage = "Evaluation interrupted due to application restart"

// recoverInterruptedEvaluationRuns closes evaluation runs that were left in
// pending or running state when the application process stopped unexpectedly.
func recoverInterruptedEvaluationRuns(db *gorm.DB) {
	if db == nil {
		return
	}
	ctx := context.Background()
	repo := repository.NewEvaluationRepository(db)
	count, err := repo.MarkAllInterruptedRunsFailed(
		ctx,
		time.Now(),
		evaluationRestartInterruptedMessage,
	)
	if err != nil {
		logger.Warnf(ctx, "Failed to recover interrupted evaluation runs: %v", err)
		return
	}
	if count > 0 {
		logger.Infof(ctx, "Recovered %d interrupted evaluation run(s) after application restart", count)
	}
}
