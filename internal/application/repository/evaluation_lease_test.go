package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func TestEvaluationLeaseFencesProgressAndTerminalWrites(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	repo := NewEvaluationRepository(db)
	leases := repo.(interfaces.EvaluationLeaseRepository)
	ctx := context.Background()
	detail := newEvaluationRepositoryTestDetail("owned-run", 7)
	detail.LeaseOwnerID = "execution-a"
	require.NoError(t, repo.CreateRun(ctx, detail, "temporary-kb"))
	var record types.EvaluationRunRecord
	require.NoError(t, db.First(&record).Error)
	require.Equal(t, detail.LeaseOwnerID, record.OwnerID)
	require.NotNil(t, record.HeartbeatAt)
	require.True(t, record.LeaseUntil.After(time.Now()))
	publicJSON, err := json.Marshal(detail)
	require.NoError(t, err)
	require.NotContains(t, string(publicJSON), detail.LeaseOwnerID)
	loaded, err := repo.GetRun(ctx, 7, detail.Task.ID)
	require.NoError(t, err)
	require.Empty(t, loaded.LeaseOwnerID)
	require.Error(t, repo.UpdateRun(ctx, loaded), "a read snapshot must not bypass the owner")
	for _, identity := range []struct {
		tenant uint64
		owner  string
	}{{8, "execution-a"}, {7, "execution-b"}} {
		ok, err := leases.RenewEvaluationRunLease(ctx, identity.tenant, detail.Task.ID, identity.owner, time.Now().Add(time.Minute))
		require.NoError(t, err)
		require.False(t, ok)
		require.NoError(t, leases.ReleaseEvaluationRunLease(ctx, identity.tenant, detail.Task.ID, identity.owner))
	}
	require.NoError(t, repo.UpdateRun(ctx, detail), "wrong-owner releases must preserve ownership")

	// Simulate a crashed/stalled worker. Once the lease expires, neither a
	// late heartbeat nor any run/case write can resurrect it.
	require.NoError(t, db.Model(&record).Update("lease_until", time.Now().Add(-time.Minute)).Error)
	ok, err := leases.RenewEvaluationRunLease(ctx, 7, detail.Task.ID, detail.LeaseOwnerID, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.False(t, ok)
	require.Error(t, repo.UpdateRun(ctx, detail))
	caseResult := &types.EvaluationCaseResult{CaseID: "late-case"}
	require.Error(t, repo.SaveCaseProgress(ctx, detail, caseResult))
	count, err := leases.RecoverExpiredEvaluationRuns(ctx, time.Now(), "expired")
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	detail.Task.Status = types.EvaluationStatueSuccess
	detail.Result.Cases = []types.EvaluationCaseResult{*caseResult}
	require.Error(t, repo.SaveTerminalRun(ctx, detail))
	var cases int64
	require.NoError(t, db.Model(&types.EvaluationRunCaseRecord{}).Count(&cases).Error)
	require.Zero(t, cases, "fenced transactions must not insert late cases")
	record = types.EvaluationRunRecord{}
	require.NoError(t, db.First(&record).Error)
	require.Equal(t, types.EvaluationRunStatusFailed, record.Status)
	require.Empty(t, record.OwnerID)
	require.Nil(t, record.LeaseUntil)
}

func TestEvaluationLeaseRecoveryRechecksConcurrentChanges(t *testing.T) {
	for _, change := range []string{"heartbeat", "progress"} {
		t.Run(change, func(t *testing.T) {
			db := newEvaluationRepositoryTestDB(t)
			repo := NewEvaluationRepository(db).(*evaluationRepository)
			ctx := context.Background()
			detail := newEvaluationRepositoryTestDetail("scan-race", 7)
			detail.LeaseOwnerID = "execution-a"
			require.NoError(t, repo.CreateRun(ctx, detail, "temporary-kb"))
			var scanned types.EvaluationRunRecord
			require.NoError(t, db.First(&scanned).Error)
			// A sweep has selected this row at a future cutoff. Before it can
			// update, the owner renews or persists newer case progress.
			cutoff := time.Now().Add(3 * time.Minute)
			if change == "heartbeat" {
				ok, err := repo.RenewEvaluationRunLease(ctx, 7, detail.Task.ID, detail.LeaseOwnerID, cutoff.Add(time.Minute))
				require.NoError(t, err)
				require.True(t, ok)
			} else {
				detail.Task.Finished = 1
				require.NoError(t, repo.UpdateRun(ctx, detail))
			}
			count, err := repo.markInterruptedRecords(ctx, []types.EvaluationRunRecord{scanned}, cutoff, "restart")
			require.NoError(t, err)
			require.Zero(t, count)
			if change == "progress" {
				// The next scan closes the latest snapshot as partial.
				count, err = repo.RecoverExpiredEvaluationRuns(ctx, cutoff, "expired")
				require.NoError(t, err)
				require.Equal(t, int64(1), count)
				var recovered types.EvaluationRunRecord
				require.NoError(t, db.First(&recovered).Error)
				require.Equal(t, types.EvaluationRunStatusPartial, recovered.Status)
			}
		})
	}
}
