package container

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoverInterruptedEvaluationRunsOnStartup(t *testing.T) {
	db := setupResetPendingDB(t)
	require.NoError(t, db.AutoMigrate(&types.EvaluationRunRecord{}, &types.EvaluationRunCaseRecord{}))
	startedAt := time.Date(2026, 8, 31, 4, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&types.EvaluationRunRecord{
		RunID:              "startup-pending",
		TenantID:           7,
		DatasetID:          "dataset",
		DatasetVersion:     "1",
		DatasetFingerprint: "fingerprint",
		ConfigHash:         "config",
		EmbeddingModelID:   "embedding",
		ChatModelID:        "chat",
		Status:             types.EvaluationRunStatusPending,
		ConfigSnapshot:     types.JSON(`{}`),
		ParamsSnapshot:     types.JSON(`{}`),
		ResultSnapshot:     types.JSON(`{"run":{"status":"pending"}}`),
		StartedAt:          startedAt,
		CreatedAt:          startedAt,
		UpdatedAt:          startedAt,
		Revision:           1,
	}).Error)
	require.NoError(t, db.Create(&types.EvaluationRunRecord{
		RunID:              "startup-running",
		TenantID:           8,
		DatasetID:          "dataset",
		DatasetVersion:     "1",
		DatasetFingerprint: "fingerprint",
		ConfigHash:         "config",
		EmbeddingModelID:   "embedding",
		ChatModelID:        "chat",
		Status:             types.EvaluationRunStatusRunning,
		Total:              2,
		Finished:           1,
		ConfigSnapshot:     types.JSON(`{}`),
		ParamsSnapshot:     types.JSON(`{}`),
		ResultSnapshot:     types.JSON(`{"run":{"status":"running"}}`),
		StartedAt:          startedAt,
		CreatedAt:          startedAt,
		UpdatedAt:          startedAt,
		Revision:           1,
	}).Error)
	require.NoError(t, db.Create(&types.EvaluationRunRecord{
		RunID:              "startup-success",
		TenantID:           7,
		DatasetID:          "dataset",
		DatasetVersion:     "1",
		DatasetFingerprint: "fingerprint",
		ConfigHash:         "config",
		EmbeddingModelID:   "embedding",
		ChatModelID:        "chat",
		Status:             types.EvaluationRunStatusSuccess,
		ConfigSnapshot:     types.JSON(`{}`),
		ParamsSnapshot:     types.JSON(`{}`),
		ResultSnapshot:     types.JSON(`{"run":{"status":"success"}}`),
		StartedAt:          startedAt,
		CreatedAt:          startedAt,
		UpdatedAt:          startedAt,
		Revision:           1,
	}).Error)

	recoverInterruptedEvaluationRuns(db)

	var runs []types.EvaluationRunRecord
	require.NoError(t, db.Order("run_id ASC").Find(&runs).Error)
	require.Len(t, runs, 3)
	assert.Equal(t, types.EvaluationRunStatusFailed, runs[0].Status)
	assert.Equal(t, types.EvaluationRunStatusPartial, runs[1].Status)
	assert.Equal(t, types.EvaluationRunStatusSuccess, runs[2].Status)
	var recovered types.EvaluationRunRecord
	require.NoError(t, db.Where("run_id = ?", "startup-pending").Take(&recovered).Error)
	assert.Equal(t, evaluationRestartInterruptedMessage, recovered.ErrorMessage)
	assert.NotNil(t, recovered.CompletedAt)
	assert.Equal(t, uint64(2), recovered.Revision)
}

func TestRecoverInterruptedEvaluationRunsPreservesOtherInstance(t *testing.T) {
	db := setupResetPendingDB(t)
	require.NoError(t, db.AutoMigrate(&types.EvaluationRunRecord{}, &types.EvaluationRunCaseRecord{}))
	now := time.Now()
	leaseUntil := now.Add(time.Minute)
	run := &types.EvaluationRunRecord{
		RunID: "other-instance", TenantID: 7, Status: types.EvaluationRunStatusRunning,
		ConfigSnapshot: types.JSON(`{}`), ParamsSnapshot: types.JSON(`{}`), ResultSnapshot: types.JSON(`{}`),
		StartedAt: now, OwnerID: "other-owner", LeaseUntil: &leaseUntil, HeartbeatAt: &now,
	}
	require.NoError(t, db.Create(run).Error)
	recoverInterruptedEvaluationRuns(db)
	require.NoError(t, db.First(run).Error)
	assert.Equal(t, types.EvaluationRunStatusRunning, run.Status)
	assert.Equal(t, "other-owner", run.OwnerID)
	// Later sweeps close the run once its lease actually expires.
	require.NoError(t, db.Model(run).Update("lease_until", now.Add(-time.Minute)).Error)
	recoverInterruptedEvaluationRuns(db)
	require.NoError(t, db.First(run).Error)
	assert.Equal(t, types.EvaluationRunStatusFailed, run.Status)
}
