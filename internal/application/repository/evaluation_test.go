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
	typesinterfaces "github.com/Tencent/WeKnora/internal/types/interfaces"
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

func TestEvaluationRepositoryLeasesProtectActiveRun(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	repo := NewEvaluationRepository(db)
	detail := newEvaluationRepositoryTestDetail("lease-run", 7)
	detail.LeaseOwnerID = "instance-a"
	if err := repo.CreateRun(context.Background(), detail, "temporary-kb"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	leaseRepo := repo.(typesinterfaces.EvaluationLeaseRepository)
	now := time.Now()
	claimed, err := leaseRepo.RenewEvaluationRunLease(context.Background(), 7, "lease-run", "instance-b", now.Add(time.Minute))
	if err != nil || claimed {
		t.Fatalf("active lease was stolen: claimed=%v err=%v", claimed, err)
	}
	count, err := leaseRepo.RecoverExpiredEvaluationRuns(context.Background(), now, "restart")
	if err != nil || count != 0 {
		t.Fatalf("active lease recovered: count=%d err=%v", count, err)
	}
	ok, err := leaseRepo.RenewEvaluationRunLease(context.Background(), 7, "lease-run", "instance-a", now.Add(2*time.Minute))
	if err != nil || !ok {
		t.Fatalf("renew lease: ok=%v err=%v", ok, err)
	}
}

func containsEvaluationSnapshotText(snapshot types.JSON, value string) bool {
	return len(value) > 0 && strings.Contains(string(snapshot), value)
}

func TestMarshalEvaluationRunResultSnapshotExcludesCases(t *testing.T) {
	snapshot, err := marshalEvaluationRunResultSnapshot(&types.EvaluationRunResult{
		SchemaVersion: types.EvaluationResultSchemaVersion,
		Cases: []types.EvaluationCaseResult{
			{CaseID: "10", Evidence: types.EvaluationCaseEvidence{QuestionFingerprint: "sha256:question"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal aggregate result: %v", err)
	}
	var result types.EvaluationRunResult
	if err := json.Unmarshal(snapshot, &result); err != nil {
		t.Fatalf("decode aggregate result: %v", err)
	}
	if len(result.Cases) != 0 || strings.Contains(string(snapshot), "sha256:question") {
		t.Fatalf("aggregate result contains case evidence: %s", snapshot)
	}
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
	if len(loaded.Result.Cases) != 1 || loaded.Result.Cases[0].CaseID != "10" ||
		loaded.Result.Cases[0].Evidence.QID != 10 {
		t.Fatalf("case table was not assembled into the run response: %#v", loaded.Result.Cases)
	}
	var runRecord types.EvaluationRunRecord
	if err := db.Where("tenant_id = ? AND run_id = ?", 7, detail.Task.ID).Take(&runRecord).Error; err != nil {
		t.Fatalf("load run record: %v", err)
	}
	var aggregateResult types.EvaluationRunResult
	if err := json.Unmarshal(runRecord.ResultSnapshot, &aggregateResult); err != nil {
		t.Fatalf("decode aggregate result snapshot: %v", err)
	}
	if len(aggregateResult.Cases) != 0 || strings.Contains(string(runRecord.ResultSnapshot), "sha256:question") {
		t.Fatalf("run snapshot duplicated case evidence: %s", runRecord.ResultSnapshot)
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
	detail.Result.Cases = []types.EvaluationCaseResult{
		{CaseID: "10", Status: types.EvaluationRunStatusSuccess, StartedAt: detail.Task.StartTime},
		{CaseID: "2", Status: types.EvaluationRunStatusSuccess, StartedAt: detail.Task.StartTime},
	}
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
	if len(loaded.Result.Cases) != 2 || loaded.Result.Cases[0].CaseID != "2" ||
		loaded.Result.Cases[1].CaseID != "10" {
		t.Fatalf("persisted cases were not assembled in numeric order: %#v", loaded.Result.Cases)
	}
}

func TestEvaluationRepositoryMarksInterruptedRunsFailed(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	repo := NewEvaluationRepository(db)
	pending := newEvaluationRepositoryTestDetail("evaluation-pending", 7)
	success := newEvaluationRepositoryTestDetail("evaluation-success", 7)
	otherTenantPending := newEvaluationRepositoryTestDetail("evaluation-other-tenant", 8)
	success.Task.Status = types.EvaluationStatueSuccess
	success.Result.Run.Status = types.EvaluationRunStatusSuccess
	if err := repo.CreateRun(context.Background(), pending, "temporary-pending"); err != nil {
		t.Fatalf("create pending run: %v", err)
	}
	if err := repo.CreateRun(context.Background(), success, "temporary-success"); err != nil {
		t.Fatalf("create success run: %v", err)
	}
	if err := repo.CreateRun(context.Background(), otherTenantPending, "temporary-other-tenant"); err != nil {
		t.Fatalf("create other tenant run: %v", err)
	}

	completedAt := time.Date(2026, 8, 25, 2, 0, 0, 0, time.UTC)
	count, err := repo.MarkInterruptedRunsFailed(context.Background(), 7, completedAt, "process_interrupted")
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
	loadedOtherTenant, err := repo.GetRun(context.Background(), 8, otherTenantPending.Task.ID)
	if err != nil {
		t.Fatalf("get other tenant run: %v", err)
	}
	if loadedOtherTenant.Task.Status != types.EvaluationStatuePending {
		t.Fatalf("other tenant run was changed: %#v", loadedOtherTenant.Task)
	}
}

func TestEvaluationRepositoryMarksAllInterruptedRunsWithTerminalState(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	repo := NewEvaluationRepository(db)

	pending := newEvaluationRepositoryTestDetail("evaluation-all-pending", 7)
	running := newEvaluationRepositoryTestDetail("evaluation-all-running", 7)
	running.Task.Status = types.EvaluationStatueRunning
	running.Result.Run.Status = types.EvaluationRunStatusRunning
	running.Task.Finished = 1

	terminal := newEvaluationRepositoryTestDetail("evaluation-all-success", 7)
	terminal.Task.Status = types.EvaluationStatueSuccess
	terminal.Result.Run.Status = types.EvaluationRunStatusSuccess
	otherTenant := newEvaluationRepositoryTestDetail("evaluation-all-other-tenant", 8)
	otherTenant.Task.Status = types.EvaluationStatueRunning
	otherTenant.Result.Run.Status = types.EvaluationRunStatusRunning
	otherTenant.Task.Finished = 1

	for _, detail := range []*types.EvaluationDetail{pending, running, terminal, otherTenant} {
		if err := repo.CreateRun(context.Background(), detail, "temporary-"+detail.Task.ID); err != nil {
			t.Fatalf("create run %s: %v", detail.Task.ID, err)
		}
	}

	completedAt := time.Date(2026, 8, 31, 5, 0, 0, 0, time.UTC)
	count, err := repo.MarkAllInterruptedRunsFailed(
		context.Background(), completedAt, "restart interrupted api_key=secret",
	)
	if err != nil {
		t.Fatalf("mark all interrupted runs: %v", err)
	}
	if count != 3 {
		t.Fatalf("marked %d runs, want 3", count)
	}

	var records []types.EvaluationRunRecord
	if err := db.Order("run_id ASC").Find(&records).Error; err != nil {
		t.Fatalf("load recovered runs: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("loaded %d runs, want 4", len(records))
	}
	byID := make(map[string]types.EvaluationRunRecord, len(records))
	for _, record := range records {
		byID[record.RunID] = record
	}
	if byID[pending.Task.ID].Status != types.EvaluationRunStatusFailed {
		t.Fatalf("pending run status = %s, want failed", byID[pending.Task.ID].Status)
	}
	if byID[running.Task.ID].Status != types.EvaluationRunStatusPartial {
		t.Fatalf("running run status = %s, want partial", byID[running.Task.ID].Status)
	}
	if byID[otherTenant.Task.ID].Status != types.EvaluationRunStatusPartial {
		t.Fatalf("other-tenant run status = %s, want partial", byID[otherTenant.Task.ID].Status)
	}
	var recoveredSnapshot types.EvaluationRunResult
	if err := json.Unmarshal(byID[running.Task.ID].ResultSnapshot, &recoveredSnapshot); err != nil {
		t.Fatalf("decode recovered result snapshot: %v", err)
	}
	if recoveredSnapshot.Run.Status != types.EvaluationRunStatusPartial ||
		recoveredSnapshot.Run.CompletedAt == nil || !recoveredSnapshot.Run.CompletedAt.Equal(completedAt) {
		t.Fatalf("result snapshot was not closed consistently: %#v", recoveredSnapshot.Run)
	}
	if byID[terminal.Task.ID].Status != types.EvaluationRunStatusSuccess {
		t.Fatalf("terminal run status changed to %s", byID[terminal.Task.ID].Status)
	}
	for _, runID := range []string{pending.Task.ID, running.Task.ID, otherTenant.Task.ID} {
		record := byID[runID]
		if record.CompletedAt == nil || !record.CompletedAt.Equal(completedAt) {
			t.Fatalf("run %s completed_at = %v, want %v", runID, record.CompletedAt, completedAt)
		}
		if record.ErrorMessage != "restart interrupted api_key=[REDACTED]" {
			t.Fatalf("run %s error message = %q, want sanitized message", runID, record.ErrorMessage)
		}
		if record.Revision != 2 {
			t.Fatalf("run %s revision = %d, want 2", runID, record.Revision)
		}
	}

	loaded, err := repo.GetRun(context.Background(), 7, running.Task.ID)
	if err != nil {
		t.Fatalf("get recovered partial run: %v", err)
	}
	if loaded.Task.Status != types.EvaluationStatueFailed ||
		loaded.Result.Run.Status != types.EvaluationRunStatusPartial || loaded.Result.Run.CompletedAt == nil {
		t.Fatalf("recovered partial run was not exposed consistently: %#v", loaded.Result.Run)
	}
	overview, err := repo.GetRunOverview(context.Background(), 7, running.Task.ID)
	if err != nil {
		t.Fatalf("get recovered run overview: %v", err)
	}
	if overview.Summary.Status != types.EvaluationRunStatusPartial {
		t.Fatalf("recovered run overview status = %s, want partial", overview.Summary.Status)
	}

	count, err = repo.MarkAllInterruptedRunsFailed(context.Background(), completedAt, "second pass")
	if err != nil {
		t.Fatalf("repeat recovery pass: %v", err)
	}
	if count != 0 {
		t.Fatalf("repeat recovery marked %d runs, want 0", count)
	}
}

func TestEvaluationRepositorySaveTerminalRunUpsertsCases(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	repo := NewEvaluationRepository(db)
	detail := newEvaluationRepositoryTestDetail("evaluation-terminal-retry", 7)
	detail.Task.Status = types.EvaluationStatueSuccess
	detail.Result.Run.Status = types.EvaluationRunStatusSuccess
	completedAt := detail.Task.StartTime.Add(time.Minute)
	detail.Result.Run.CompletedAt = &completedAt
	detail.Result.Cases = []types.EvaluationCaseResult{
		{CaseID: "case-1", Status: types.EvaluationRunStatusSuccess, StartedAt: detail.Task.StartTime, CompletedAt: &completedAt},
	}
	if err := repo.CreateRun(context.Background(), detail, "temporary-terminal-retry"); err != nil {
		t.Fatalf("create terminal retry run: %v", err)
	}
	if err := repo.SaveTerminalRun(context.Background(), detail); err != nil {
		t.Fatalf("save terminal run: %v", err)
	}
	if err := repo.SaveTerminalRun(context.Background(), detail); err != nil {
		t.Fatalf("repeat terminal run save: %v", err)
	}

	var count int64
	if err := db.Model(&types.EvaluationRunCaseRecord{}).
		Where("tenant_id = ? AND run_id = ?", 7, detail.Task.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("count persisted cases: %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted %d cases after retry, want 1", count)
	}
	loaded, err := repo.GetRun(context.Background(), 7, detail.Task.ID)
	if err != nil {
		t.Fatalf("get terminal retry run: %v", err)
	}
	if loaded.Result.Run.Status != types.EvaluationRunStatusSuccess || len(loaded.Result.Cases) != 1 {
		t.Fatalf("terminal retry changed durable state: %#v", loaded.Result)
	}
}
