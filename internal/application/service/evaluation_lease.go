package service

import (
	"context"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// startEvaluationHeartbeat returns a stop function that waits for any in-flight
// renewal before releasing ownership. Losing renewal cancels provider work;
// repository writes also check the owner and deadline to fence stale workers.
func startEvaluationHeartbeat(ctx context.Context, repo interfaces.EvaluationLeaseRepository, tenantID uint64, runID, ownerID string, interval time.Duration, cancelRun context.CancelFunc) func() {
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				renewCtx, cancel := context.WithTimeout(heartbeatCtx, 10*time.Second)
				owned, err := repo.RenewEvaluationRunLease(renewCtx, tenantID, runID, ownerID, time.Now().Add(types.EvaluationRunLeaseDuration))
				cancel()
				if heartbeatCtx.Err() != nil {
					return
				}
				if err != nil || !owned {
					logger.Warnf(ctx, "Evaluation lease renewal failed; stopping run=%s err=%v", runID, err)
					cancelRun()
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(cancelHeartbeat)
		<-done
	}
}
