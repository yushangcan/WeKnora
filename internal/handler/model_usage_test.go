package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type modelUsageServiceStub struct {
	interfaces.ModelUsageService
	list    func(context.Context, types.ModelUsageFilter) (*types.ModelUsageEventPage, error)
	summary func(context.Context, types.ModelUsageFilter) (*types.ModelUsageSummary, error)
}

func (s *modelUsageServiceStub) List(ctx context.Context, filter types.ModelUsageFilter) (*types.ModelUsageEventPage, error) {
	return s.list(ctx, filter)
}
func (s *modelUsageServiceStub) Summary(ctx context.Context, filter types.ModelUsageFilter) (*types.ModelUsageSummary, error) {
	return s.summary(ctx, filter)
}

func modelUsageTestRouter(h *ModelUsageHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/usage/events", h.ListUsageEvents)
	r.GET("/usage/summary", h.GetUsageSummary)
	return r
}

func TestModelUsageHandlerParsesFiltersAndPagination(t *testing.T) {
	var captured types.ModelUsageFilter
	service := &modelUsageServiceStub{
		list: func(_ context.Context, filter types.ModelUsageFilter) (*types.ModelUsageEventPage, error) {
			captured = filter
			return &types.ModelUsageEventPage{Items: []types.ModelUsageEvent{}, Page: filter.Page, PageSize: filter.PageSize}, nil
		},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/usage/events?page=2&page_size=25&model_id=chat-1&model_type=KnowledgeQA&success=false&started_from=2026-08-30T00:00:00Z", nil)
	modelUsageTestRouter(NewModelUsageHandler(service)).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if captured.Page != 2 || captured.PageSize != 25 || captured.ModelID != "chat-1" || captured.ModelType != types.ModelTypeKnowledgeQA || captured.Success == nil || *captured.Success {
		t.Fatalf("filters were not parsed: %#v", captured)
	}
}

func TestModelUsageHandlerRejectsInvalidQuery(t *testing.T) {
	service := &modelUsageServiceStub{
		list: func(context.Context, types.ModelUsageFilter) (*types.ModelUsageEventPage, error) {
			t.Fatal("service must not be called")
			return nil, nil
		},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/usage/events?page_size=101", nil)
	modelUsageTestRouter(NewModelUsageHandler(service)).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestModelUsageHandlerReturnsSummary(t *testing.T) {
	service := &modelUsageServiceStub{
		summary: func(context.Context, types.ModelUsageFilter) (*types.ModelUsageSummary, error) {
			return &types.ModelUsageSummary{TotalCalls: 3, CostStatus: types.ModelUsageCostStatusUnavailable, ByModel: []types.ModelUsageByModel{}}, nil
		},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/usage/summary", nil)
	modelUsageTestRouter(NewModelUsageHandler(service)).ServeHTTP(w, req)
	if w.Code != http.StatusOK || !containsModelUsageText(w.Body.String(), "total_calls") {
		t.Fatalf("response = %d %s", w.Code, w.Body.String())
	}
}

func containsModelUsageText(body, value string) bool {
	for i := 0; i+len(value) <= len(body); i++ {
		if body[i:i+len(value)] == value {
			return true
		}
	}
	return false
}
