package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type evaluationHistoryServiceStub struct {
	interfaces.EvaluationService
	list       func(context.Context, types.EvaluationRunListFilter) (*types.EvaluationRunPage, error)
	get        func(context.Context, string) (*types.EvaluationRunOverview, error)
	listCases  func(context.Context, string, types.EvaluationRunStatus, int, int) (*types.EvaluationCasePage, error)
	comparison func(context.Context, string, []string) (*types.EvaluationComparison, error)
}

func (s *evaluationHistoryServiceStub) ListEvaluationRuns(
	ctx context.Context,
	filter types.EvaluationRunListFilter,
) (*types.EvaluationRunPage, error) {
	return s.list(ctx, filter)
}

func (s *evaluationHistoryServiceStub) GetEvaluationRun(
	ctx context.Context,
	runID string,
) (*types.EvaluationRunOverview, error) {
	return s.get(ctx, runID)
}

func (s *evaluationHistoryServiceStub) ListEvaluationRunCases(
	ctx context.Context,
	runID string,
	status types.EvaluationRunStatus,
	page int,
	pageSize int,
) (*types.EvaluationCasePage, error) {
	return s.listCases(ctx, runID, status, page, pageSize)
}

func (s *evaluationHistoryServiceStub) CompareEvaluationRuns(
	ctx context.Context,
	baselineID string,
	runIDs []string,
) (*types.EvaluationComparison, error) {
	return s.comparison(ctx, baselineID, runIDs)
}

func evaluationHistoryTestRouter(h *EvaluationHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.GET("/evaluation/runs", h.ListEvaluationRuns)
	router.GET("/evaluation/runs/:run_id", h.GetEvaluationRun)
	router.GET("/evaluation/runs/:run_id/cases", h.ListEvaluationRunCases)
	router.GET("/evaluation/comparison", h.CompareEvaluationRuns)
	return router
}

func TestListEvaluationRunsParsesFiltersAndPagination(t *testing.T) {
	var captured types.EvaluationRunListFilter
	service := &evaluationHistoryServiceStub{list: func(
		_ context.Context,
		filter types.EvaluationRunListFilter,
	) (*types.EvaluationRunPage, error) {
		captured = filter
		return &types.EvaluationRunPage{Items: []types.EvaluationRunSummary{}, Page: filter.Page, PageSize: filter.PageSize}, nil
	}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/evaluation/runs?page=2&page_size=25&status=success&chat_model_id=chat-1&started_from=2026-08-27T00:00:00Z",
		nil,
	)
	evaluationHistoryTestRouter(NewEvaluationHandler(service)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if captured.Page != 2 || captured.PageSize != 25 || captured.Status != types.EvaluationRunStatusSuccess ||
		captured.ChatModelID != "chat-1" || captured.StartedFrom == nil {
		t.Fatalf("filters were not parsed: %#v", captured)
	}
}

func TestListEvaluationRunsRejectsInvalidTime(t *testing.T) {
	service := &evaluationHistoryServiceStub{list: func(
		_ context.Context,
		_ types.EvaluationRunListFilter,
	) (*types.EvaluationRunPage, error) {
		t.Fatal("service must not be called")
		return nil, nil
	}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/evaluation/runs?started_from=yesterday", nil)
	evaluationHistoryTestRouter(NewEvaluationHandler(service)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestListEvaluationRunCasesPassesIndependentPage(t *testing.T) {
	service := &evaluationHistoryServiceStub{listCases: func(
		_ context.Context,
		runID string,
		status types.EvaluationRunStatus,
		page int,
		pageSize int,
	) (*types.EvaluationCasePage, error) {
		if runID != "run-1" || status != types.EvaluationRunStatusFailed || page != 3 || pageSize != 10 {
			t.Fatalf("unexpected case query: %s %s %d %d", runID, status, page, pageSize)
		}
		return &types.EvaluationCasePage{Items: []types.EvaluationCaseResult{}, Page: page, PageSize: pageSize}, nil
	}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/evaluation/runs/run-1/cases?page=3&page_size=10&status=failed", nil)
	evaluationHistoryTestRouter(NewEvaluationHandler(service)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestCompareEvaluationRunsParsesCommaSeparatedIDs(t *testing.T) {
	service := &evaluationHistoryServiceStub{comparison: func(
		_ context.Context,
		baselineID string,
		runIDs []string,
	) (*types.EvaluationComparison, error) {
		if baselineID != "run-a" || strings.Join(runIDs, ",") != "run-a,run-b" {
			t.Fatalf("unexpected comparison query: %q %#v", baselineID, runIDs)
		}
		return &types.EvaluationComparison{BaselineID: baselineID, Runs: []types.EvaluationRunComparison{}}, nil
	}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/evaluation/comparison?baseline_id=run-a&run_ids=run-a,run-b", nil)
	evaluationHistoryTestRouter(NewEvaluationHandler(service)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetEvaluationRunMapsMissingRunToNotFound(t *testing.T) {
	service := &evaluationHistoryServiceStub{get: func(context.Context, string) (*types.EvaluationRunOverview, error) {
		return nil, gorm.ErrRecordNotFound
	}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/evaluation/runs/missing", nil)
	evaluationHistoryTestRouter(NewEvaluationHandler(service)).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}
