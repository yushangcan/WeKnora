package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

func createEvaluationHistoryRun(
	t *testing.T,
	db *gorm.DB,
	runID string,
	tenantID uint64,
	createdAt time.Time,
	chatModelID string,
	status types.EvaluationRunStatus,
	caseStatuses ...types.EvaluationRunStatus,
) {
	t.Helper()
	repo := NewEvaluationRepository(db)
	detail := newEvaluationRepositoryTestDetail(runID, tenantID)
	detail.Config.Models.Chat = types.EvaluationModelConfig{ID: chatModelID, Name: chatModelID}
	detail.Config.Runtime.MetricVersion = types.EvaluationMetricVersion
	detail.Config.Runtime.ResultVersion = types.EvaluationResultSchemaVersion
	detail.Config.ConfigHash = "sha256:" + runID
	detail.Task.Total = len(caseStatuses)
	detail.Task.Finished = len(caseStatuses)
	detail.Result.Run.Status = status
	detail.Result.Retrieval = &types.EvaluationRetrievalResult{Precision: 0.5}
	detail.Result.Answer = &types.EvaluationAnswerResult{ROUGEL: 0.4}
	detail.Result.Timing.TotalWallTimeMS = 1200
	amount := 0.25
	detail.Result.Cost = types.EvaluationCostResult{
		Status: types.EvaluationCostStatusComplete, Amount: &amount, Currency: "USD", PricingVersion: "v1",
	}
	if status == types.EvaluationRunStatusSuccess {
		detail.Task.Status = types.EvaluationStatueSuccess
	} else if status == types.EvaluationRunStatusFailed || status == types.EvaluationRunStatusPartial {
		detail.Task.Status = types.EvaluationStatueFailed
	}
	completedAt := createdAt.Add(time.Second)
	detail.Result.Run.CompletedAt = &completedAt
	for i, caseStatus := range caseStatuses {
		detail.Result.Cases = append(detail.Result.Cases, types.EvaluationCaseResult{
			CaseID:    string(rune('1' + i)),
			Status:    caseStatus,
			StartedAt: createdAt,
			Evidence:  types.EvaluationCaseEvidence{QID: i + 1},
			Warnings:  []types.EvaluationWarning{},
		})
	}
	if err := repo.CreateRun(context.Background(), detail, "temporary-"+runID); err != nil {
		t.Fatalf("create history run: %v", err)
	}
	if err := repo.SaveTerminalRun(context.Background(), detail); err != nil {
		t.Fatalf("save history run: %v", err)
	}
	if err := db.Model(&types.EvaluationRunRecord{}).Where("run_id = ?", runID).
		Updates(map[string]interface{}{"created_at": createdAt, "updated_at": completedAt}).Error; err != nil {
		t.Fatalf("set history timestamps: %v", err)
	}
}

func TestEvaluationRepositoryListsRunsWithStableTenantScopedPagination(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	base := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	createEvaluationHistoryRun(t, db, "run-a", 7, base, "chat-a", types.EvaluationRunStatusSuccess,
		types.EvaluationRunStatusSuccess, types.EvaluationRunStatusFailed)
	createEvaluationHistoryRun(t, db, "run-b", 7, base.Add(time.Minute), "chat-b", types.EvaluationRunStatusSuccess,
		types.EvaluationRunStatusSuccess)
	createEvaluationHistoryRun(t, db, "run-other", 8, base.Add(2*time.Minute), "chat-b", types.EvaluationRunStatusSuccess,
		types.EvaluationRunStatusSuccess)

	repo := NewEvaluationRepository(db)
	page, err := repo.ListRuns(context.Background(), 7, types.EvaluationRunListFilter{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("list evaluation runs: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].RunID != "run-b" {
		t.Fatalf("unexpected first page: %#v", page)
	}
	if page.Items[0].Progress.Cases.Total != 1 || page.Items[0].Progress.Cases.Success != 1 {
		t.Fatalf("case counts were not batched into summary: %#v", page.Items[0].Progress.Cases)
	}

	filtered, err := repo.ListRuns(context.Background(), 7, types.EvaluationRunListFilter{
		ChatModelID: "chat-a", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("filter evaluation runs: %v", err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].RunID != "run-a" {
		t.Fatalf("model filter leaked or missed rows: %#v", filtered)
	}
	if filtered.Items[0].Progress.Cases.Success != 1 || filtered.Items[0].Progress.Cases.Failed != 1 {
		t.Fatalf("case status counts changed: %#v", filtered.Items[0].Progress.Cases)
	}
	partial, err := repo.ListRuns(context.Background(), 7, types.EvaluationRunListFilter{
		Status: types.EvaluationRunStatusPartial, Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("filter partial evaluation runs: %v", err)
	}
	if partial.Total != 0 {
		t.Fatalf("successful runs were returned by the partial filter: %#v", partial)
	}
}

func TestEvaluationRepositoryListsRunCasesSeparately(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	base := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	createEvaluationHistoryRun(t, db, "run-cases", 7, base, "chat-a", types.EvaluationRunStatusPartial,
		types.EvaluationRunStatusSuccess, types.EvaluationRunStatusFailed, types.EvaluationRunStatusSuccess)
	repo := NewEvaluationRepository(db)
	partialRuns, err := repo.ListRuns(context.Background(), 7, types.EvaluationRunListFilter{
		Status: types.EvaluationRunStatusPartial, Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("filter partial evaluation runs: %v", err)
	}
	if partialRuns.Total != 1 || len(partialRuns.Items) != 1 || partialRuns.Items[0].RunID != "run-cases" {
		t.Fatalf("partial run was not persisted as partial: %#v", partialRuns)
	}

	page, err := repo.ListRunCases(context.Background(), 7, "run-cases", "", 2, 2)
	if err != nil {
		t.Fatalf("list evaluation cases: %v", err)
	}
	if page.Total != 3 || len(page.Items) != 1 || page.Items[0].CaseID != "3" {
		t.Fatalf("unexpected case page: %#v", page)
	}
	failed, err := repo.ListRunCases(
		context.Background(), 7, "run-cases", types.EvaluationRunStatusFailed, 1, 20,
	)
	if err != nil {
		t.Fatalf("filter failed cases: %v", err)
	}
	if failed.Total != 1 || len(failed.Items) != 1 || failed.Items[0].Status != types.EvaluationRunStatusFailed {
		t.Fatalf("unexpected failed case page: %#v", failed)
	}
	if _, err := repo.ListRunCases(context.Background(), 8, "run-cases", "", 1, 20); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant case list returned %v", err)
	}
}

func TestEvaluationRepositoryBatchLoadsOnlyRequestedTenantRuns(t *testing.T) {
	db := newEvaluationRepositoryTestDB(t)
	base := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	createEvaluationHistoryRun(t, db, "run-tenant-7", 7, base, "chat-a", types.EvaluationRunStatusSuccess)
	createEvaluationHistoryRun(t, db, "run-tenant-8", 8, base, "chat-a", types.EvaluationRunStatusSuccess)
	repo := NewEvaluationRepository(db)

	runs, err := repo.GetRunOverviews(context.Background(), 7, []string{"run-tenant-7", "run-tenant-8"})
	if err != nil {
		t.Fatalf("batch load evaluation runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Summary.RunID != "run-tenant-7" {
		t.Fatalf("batch load crossed tenant boundary: %#v", runs)
	}
	overview, err := repo.GetRunOverview(context.Background(), 7, "run-tenant-7")
	if err != nil {
		t.Fatalf("get run overview: %v", err)
	}
	if overview.Config == nil || overview.Summary.ConfigHash != "sha256:run-tenant-7" {
		t.Fatalf("overview lost immutable config: %#v", overview)
	}
}
