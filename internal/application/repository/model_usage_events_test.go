package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newModelUsageEventsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:model_usage_events_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.ModelUsageEvent{}))
	return db
}

func modelUsageTestEvent(tenant uint64, callID string, started time.Time) *types.ModelUsageEvent {
	prompt, completion, total := int64(10), int64(4), int64(14)
	duration := int64(25)
	return &types.ModelUsageEvent{
		CallID: callID, TenantID: tenant, ModelID: "chat-1", ModelNameSnapshot: "chat",
		ModelType: types.ModelTypeKnowledgeQA, Provider: "openai", Operation: "chat",
		Source: types.ModelUsageSourceChat, StartedAt: started, CompletedAt: &started,
		DurationMS: &duration, Success: true, ItemCount: 1,
		PromptTokens: &prompt, CompletionTokens: &completion, TotalTokens: &total,
		CacheReported: true, CacheStatus: types.ModelUsageCacheStatusHit,
		UsageSource: "provider", CostStatus: types.ModelUsageCostStatusUnavailable,
	}
}

func TestModelUsageEventsRepositoryListIsTenantScopedAndStable(t *testing.T) {
	db := newModelUsageEventsTestDB(t)
	repo := NewModelUsageEventsRepository(db)
	base := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	require.NoError(t, repo.Create(context.Background(), modelUsageTestEvent(7, "call-1", base)))
	require.NoError(t, repo.Create(context.Background(), modelUsageTestEvent(7, "call-2", base.Add(time.Minute))))
	require.NoError(t, repo.Create(context.Background(), modelUsageTestEvent(8, "call-3", base.Add(2*time.Minute))))

	page, err := repo.List(context.Background(), 7, types.ModelUsageFilter{Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, "call-2", page.Items[0].CallID)

	_, err = repo.List(context.Background(), 0, types.ModelUsageFilter{})
	require.Error(t, err)
}

func TestModelUsageEventsRepositorySummaryPreservesUnknownCostAndComputesCacheRate(t *testing.T) {
	db := newModelUsageEventsTestDB(t)
	repo := NewModelUsageEventsRepository(db)
	base := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	hit := modelUsageTestEvent(7, "call-1", base)
	miss := modelUsageTestEvent(7, "call-2", base.Add(time.Minute))
	miss.CacheStatus = types.ModelUsageCacheStatusMiss
	miss.CacheReadTokens = nil
	miss.CacheReported = true
	unreported := modelUsageTestEvent(7, "call-3", base.Add(2*time.Minute))
	unreported.CacheReported = false
	unreported.CacheStatus = types.ModelUsageCacheStatusUnreported
	require.NoError(t, repo.Create(context.Background(), hit))
	require.NoError(t, repo.Create(context.Background(), miss))
	require.NoError(t, repo.Create(context.Background(), unreported))

	summary, err := repo.Summary(context.Background(), 7, types.ModelUsageFilter{})
	require.NoError(t, err)
	require.Equal(t, int64(3), summary.TotalCalls)
	require.Equal(t, int64(2), summary.CacheReportedCalls)
	require.Equal(t, int64(1), summary.CacheHitCalls)
	require.NotNil(t, summary.CacheHitRate)
	require.InDelta(t, 0.5, *summary.CacheHitRate, 0.0001)
	require.Nil(t, summary.CostAmount)
	require.Equal(t, types.ModelUsageCostStatusUnavailable, summary.CostStatus)
	require.Len(t, summary.ByModel, 1)
}

func TestModelUsageEventsRepositorySummaryMarksPartialCost(t *testing.T) {
	db := newModelUsageEventsTestDB(t)
	repo := NewModelUsageEventsRepository(db)
	base := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	known := modelUsageTestEvent(7, "call-1", base)
	amount := 0.125
	known.CostAmount = &amount
	known.CostCurrency = "USD"
	known.CostStatus = types.ModelUsageCostStatusAvailable
	unknown := modelUsageTestEvent(7, "call-2", base.Add(time.Minute))
	require.NoError(t, repo.Create(context.Background(), known))
	require.NoError(t, repo.Create(context.Background(), unknown))

	summary, err := repo.Summary(context.Background(), 7, types.ModelUsageFilter{})
	require.NoError(t, err)
	require.NotNil(t, summary.CostAmount)
	require.InDelta(t, amount, *summary.CostAmount, 0.0001)
	require.Equal(t, types.ModelUsageCostStatusPartial, summary.CostStatus)
	require.Equal(t, "USD", summary.CostCurrency)
}
