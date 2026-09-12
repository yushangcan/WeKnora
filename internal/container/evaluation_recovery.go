package container

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

const evaluationRestartInterruptedMessage = "Evaluation interrupted because its execution lease expired"

// recoverInterruptedEvaluationRuns preserves live leases owned by other instances.
func recoverInterruptedEvaluationRuns(db *gorm.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sweepInterruptedEvaluationRuns(ctx, db)
}

func sweepInterruptedEvaluationRuns(ctx context.Context, db *gorm.DB) {
	if db == nil {
		return
	}
	repo := repository.NewEvaluationRepository(db)
	leaseRepo := repo.(interfaces.EvaluationLeaseRepository)
	count, err := leaseRepo.RecoverExpiredEvaluationRuns(ctx, time.Now(), evaluationRestartInterruptedMessage)
	if err != nil {
		logger.Warnf(ctx, "Failed to recover interrupted evaluation runs: %v", err)
		return
	}
	if count > 0 {
		logger.Infof(ctx, "Closed %d evaluation run(s) with expired execution leases", count)
	}
}

// A crashed process may still have an unexpired lease at startup. Sweeping
// periodically lets it converge to a terminal state without another restart.
func startEvaluationRecovery(db *gorm.DB, cleaner interfaces.ResourceCleaner) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweepCtx, sweepCancel := context.WithTimeout(ctx, 10*time.Second)
				sweepInterruptedEvaluationRuns(sweepCtx, db)
				sweepCancel()
			}
		}
	}()
	cleaner.RegisterWithName("EvaluationRecovery", func() error {
		cancel()
		<-done
		return nil
	})
}
