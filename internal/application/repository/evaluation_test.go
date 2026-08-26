package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newEvaluationRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&types.EvaluationRunRecord{}, &types.EvaluationRunCaseRecord{}); err != nil {
		t.Fatalf("migrate evaluation records: %v", err)
	}
	return db
}

func newEvaluationRepositoryTestDetail(runID string, tenantID uint64) *types.EvaluationDetail {
	startedAt := time.Date(2026, 8, 25, 1, 2, 3, 0, time.UTC)
	return &types.EvaluationDetail{
		Task: &types.EvaluationTask{
			ID: runID, TenantID: tenantID, DatasetID: "default",
			StartTime: startedAt, Status: types.EvaluationStatuePending, Total: 2,
		},
		Params: &types.ChatManage{PipelineRequest: types.PipelineRequest{
			EmbeddingTopK: 10, ChatModelID: "chat-1",
			SummaryConfig: types.SummaryConfig{Prompt: "private evaluation prompt"},
		}},
		Config: &types.EvaluationRunConfig{
			SchemaVersion: types.EvaluationConfigSchemaVersion,
			Dataset: types.EvaluationDatasetDescriptor{
				ID: "default", Version: "1", ContentFingerprint: "sha256:dataset", CaseCount: 2,
			},
			SourceKnowledgeBaseID: "source-kb",
			Models: types.EvaluationModelConfigSet{
				Embedding: types.EvaluationModelConfig{ID: "embedding-1"},
				Chat:      types.EvaluationModelConfig{ID: "chat-1"},
			},
			ConfigHash: "sha256:config",
		},
		Result: &types.EvaluationRunResult{
			SchemaVersion: types.EvaluationResultSchemaVersion,
			Run: types.EvaluationRunMetadata{
				RunID: runID, TenantID: tenantID, DatasetID: "default",
				StartedAt: startedAt, Status: types.EvaluationRunStatusPending,
			},
			Usage: types.EvaluationUsageResult{
				ByModel: []types.EvaluationModelUsage{}, ByPhase: []types.EvaluationPhaseUsage{},
			},
			Cost:     types.EvaluationCostResult{Amount: nil, Warnings: []types.EvaluationWarning{}},
			Cases:    []types.EvaluationCaseResult{},
			Warnings: []types.EvaluationWarning{},
		},
	}
}

func TestEvaluationRepositoryPersistsTenantScopedRun(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	repo := NewEvaluationRepository(db)
	detail := newEvaluationRepositoryTestDetail("evaluation-1", 7)

	if err := repo.CreateRun(context.Background(), detail, "temporary-kb"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	loaded, err := repo.GetRun(context.Background(), 7, detail.Task.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if loaded.Config.ConfigHash != detail.Config.ConfigHash ||
		loaded.Config.Dataset.ContentFingerprint != detail.Config.Dataset.ContentFingerprint {
		t.Fatalf("configuration snapshot changed: %#v", loaded.Config)
	}
	if loaded.Params.EmbeddingTopK != 10 || loaded.Task.Status != types.EvaluationStatuePending {
		t.Fatalf("run snapshot changed: %#v", loaded)
	}
	if _, err := repo.GetRun(context.Background(), 8, detail.Task.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant lookup returned %v", err)
	}

	var record types.EvaluationRunRecord
	if err := db.Where("run_id = ?", detail.Task.ID).Take(&record).Error; err != nil {
		t.Fatalf("load persisted row: %v", err)
	}
	if record.TemporaryKnowledgeBaseID != "temporary-kb" || record.Revision != 1 {
		t.Fatalf("run metadata was not persisted: %#v", record)
	}
	if string(record.ParamsSnapshot) == "" ||
		containsEvaluationSnapshotText(record.ParamsSnapshot, "private evaluation prompt") {
		t.Fatalf("params snapshot contains prompt text: %s", record.ParamsSnapshot)
	}
}

func containsEvaluationSnapshotText(snapshot types.JSON, value string) bool {
	return len(value) > 0 && strings.Contains(string(snapshot), value)
}

func TestEvaluationRunRecordRoundTripUsesOnlySnapshots(t *testing.T) {
	detail := newEvaluationRepositoryTestDetail("evaluation-snapshot", 7)
	record, err := newEvaluationRunRecord(detail, "temporary-kb")
	if err != nil {
		t.Fatalf("build run record: %v", err)
	}
	loaded, err := evaluationDetailFromRecord(record)
	if err != nil {
		t.Fatalf("decode run record: %v", err)
	}
	if loaded.Config.ConfigHash != detail.Config.ConfigHash ||
		loaded.Config.Models.Chat.ID != detail.Config.Models.Chat.ID ||
		loaded.Task.ID != detail.Task.ID {
		t.Fatalf("snapshot round trip changed run identity: %#v", loaded)
	}
	if loaded.Params.EmbeddingTopK != detail.Params.EmbeddingTopK {
		t.Fatalf("snapshot round trip changed numeric params: %#v", loaded.Params)
	}
	if loaded.Params.SummaryConfig.Prompt != "" {
		t.Fatalf("snapshot round trip retained prompt text: %#v", loaded.Params.SummaryConfig)
	}
}

func TestEvaluationRepositorySavesCaseAndRunAtomically(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	repo := NewEvaluationRepository(db)
	detail := newEvaluationRepositoryTestDetail("evaluation-2", 7)
	if err := repo.CreateRun(context.Background(), detail, "temporary-kb"); err != nil {
		t.Fatalf("create run: %v", err)
	}

	completedAt := detail.Task.StartTime.Add(time.Second)
	detail.Task.Status = types.EvaluationStatueRunning
	detail.Task.Finished = 1
	detail.Metric = &types.MetricResult{RetrievalMetrics: types.RetrievalMetrics{Precision: 0.5}}
	caseResult := types.EvaluationCaseResult{
		CaseID: "10", Status: types.EvaluationRunStatusSuccess,
		StartedAt: detail.Task.StartTime, CompletedAt: &completedAt, DurationMS: 1000,
		Evidence: types.EvaluationCaseEvidence{
			QID: 10, QuestionFingerprint: "sha256:question", GroundTruthPIDs: []int{1},
			MetricInputPIDs: []int{2, 1}, Metrics: &types.MetricResult{
				RetrievalMetrics: types.RetrievalMetrics{Precision: 0.5},
			},
		},
		Warnings: []types.EvaluationWarning{},
	}
	detail.Result.Run.Status = types.EvaluationRunStatusRunning
	detail.Result.Cases = []types.EvaluationCaseResult{caseResult}
	if err := repo.SaveCaseProgress(context.Background(), detail, &caseResult); err != nil {
		t.Fatalf("save case progress: %v", err)
	}

	loaded, err := repo.GetRun(context.Background(), 7, detail.Task.ID)
	if err != nil {
		t.Fatalf("get progress: %v", err)
	}
	if loaded.Task.Status != types.EvaluationStatueRunning || loaded.Task.Finished != 1 ||
		loaded.Metric == nil || loaded.Metric.RetrievalMetrics.Precision != 0.5 {
		t.Fatalf("progress snapshot changed: %#v", loaded)
	}
	var caseCount int64
	if err := db.Model(&types.EvaluationRunCaseRecord{}).
		Where("tenant_id = ? AND run_id = ?", 7, detail.Task.ID).
		Count(&caseCount).Error; err != nil {
		t.Fatalf("count cases: %v", err)
	}
	if caseCount != 1 {
		t.Fatalf("case count = %d, want 1", caseCount)
	}
	var caseRecord types.EvaluationRunCaseRecord
	if err := db.Where("tenant_id = ? AND run_id = ? AND case_id = ?", 7, detail.Task.ID, "10").
		Take(&caseRecord).Error; err != nil {
		t.Fatalf("load case record: %v", err)
	}
	var persistedCase types.EvaluationCaseResult
	if err := json.Unmarshal(caseRecord.ResultSnapshot, &persistedCase); err != nil {
		t.Fatalf("decode case result snapshot: %v", err)
	}
	if persistedCase.Evidence.QID != 10 || persistedCase.Evidence.Metrics == nil ||
		persistedCase.Evidence.Metrics.RetrievalMetrics.Precision != 0.5 {
		t.Fatalf("case audit evidence changed after persistence: %#v", persistedCase.Evidence)
	}

	detail.Task.Finished = 2
	if err := repo.SaveCaseProgress(context.Background(), detail, nil); err == nil {
		t.Fatal("expected incomplete case result to fail")
	}
	loaded, err = repo.GetRun(context.Background(), 7, detail.Task.ID)
	if err != nil {
		t.Fatalf("get run after rollback: %v", err)
	}
	if loaded.Task.Finished != 1 {
		t.Fatalf("failed transaction advanced progress to %d", loaded.Task.Finished)
	}
}

func TestEvaluationRepositoryKeepsTerminalSnapshotAfterRecreation(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	repo := NewEvaluationRepository(db)
	detail := newEvaluationRepositoryTestDetail("evaluation-3", 7)
	if err := repo.CreateRun(context.Background(), detail, "temporary-kb"); err != nil {
		t.Fatalf("create run: %v", err)
	}

	completedAt := detail.Task.StartTime.Add(2 * time.Second)
	detail.Task.Status = types.EvaluationStatueSuccess
	detail.Task.Finished = detail.Task.Total
	detail.Result.Run.Status = types.EvaluationRunStatusSuccess
	detail.Result.Run.CompletedAt = &completedAt
	detail.Result.Retrieval = &types.EvaluationRetrievalResult{Precision: 0.75}
	if err := repo.SaveTerminalRun(context.Background(), detail); err != nil {
		t.Fatalf("save terminal run: %v", err)
	}

	recreated := NewEvaluationRepository(db)
	loaded, err := recreated.GetRun(context.Background(), 7, detail.Task.ID)
	if err != nil {
		t.Fatalf("get terminal run: %v", err)
	}
	if loaded.Task.Status != types.EvaluationStatueSuccess || loaded.Result.Run.CompletedAt == nil ||
		loaded.Result.Retrieval == nil || loaded.Result.Retrieval.Precision != 0.75 {
		t.Fatalf("terminal snapshot changed: %#v", loaded)
	}
}

func TestEvaluationRepositoryMarksInterruptedRunsFailed(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	repo := NewEvaluationRepository(db)
	pending := newEvaluationRepositoryTestDetail("evaluation-pending", 7)
	success := newEvaluationRepositoryTestDetail("evaluation-success", 7)
	success.Task.Status = types.EvaluationStatueSuccess
	success.Result.Run.Status = types.EvaluationRunStatusSuccess
	if err := repo.CreateRun(context.Background(), pending, "temporary-pending"); err != nil {
		t.Fatalf("create pending run: %v", err)
	}
	if err := repo.CreateRun(context.Background(), success, "temporary-success"); err != nil {
		t.Fatalf("create success run: %v", err)
	}

	completedAt := time.Date(2026, 8, 25, 2, 0, 0, 0, time.UTC)
	count, err := repo.MarkInterruptedRunsFailed(context.Background(), completedAt, "process_interrupted")
	if err != nil {
		t.Fatalf("mark interrupted runs: %v", err)
	}
	if count != 1 {
		t.Fatalf("marked %d runs, want 1", count)
	}
	loaded, err := repo.GetRun(context.Background(), 7, pending.Task.ID)
	if err != nil {
		t.Fatalf("get interrupted run: %v", err)
	}
	if loaded.Task.Status != types.EvaluationStatueFailed || loaded.Task.ErrMsg != "process_interrupted" ||
		loaded.Result.Run.CompletedAt == nil {
		t.Fatalf("interrupted run was not closed: %#v", loaded)
	}
	loadedSuccess, err := repo.GetRun(context.Background(), 7, success.Task.ID)
	if err != nil {
		t.Fatalf("get success run: %v", err)
	}
	if loadedSuccess.Task.Status != types.EvaluationStatueSuccess {
		t.Fatalf("terminal run was changed: %#v", loadedSuccess.Task)
	}
}
