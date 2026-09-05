package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type modelUsageEventsRepository struct{ db *gorm.DB }

// NewModelUsageEventsRepository creates the tenant-scoped model usage
// repository. The name is intentionally distinct from the existing
// model_usage.go helper file used by model-deletion queries.
func NewModelUsageEventsRepository(db *gorm.DB) interfaces.ModelUsageRepository {
	return &modelUsageEventsRepository{db: db}
}

func (r *modelUsageEventsRepository) Create(ctx context.Context, event *types.ModelUsageEvent) error {
	if event == nil {
		return errors.New("model usage event is required")
	}
	if event.TenantID == 0 {
		return errors.New("tenant ID is required for model usage event")
	}
	return r.db.WithContext(ctx).Create(event).Error
}

func (r *modelUsageEventsRepository) List(ctx context.Context, tenantID uint64, filter types.ModelUsageFilter) (*types.ModelUsageEventPage, error) {
	if tenantID == 0 {
		return nil, errors.New("tenant ID is required for model usage query")
	}
	page, pageSize := normalizeModelUsagePagination(filter.Page, filter.PageSize)
	query := applyModelUsageFilters(r.db.WithContext(ctx).Model(&types.ModelUsageEvent{}).Where("tenant_id = ?", tenantID), filter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	items := make([]types.ModelUsageEvent, 0, pageSize)
	if err := query.Order("started_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, err
	}
	return &types.ModelUsageEventPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

type modelUsageAggregateRow struct {
	TotalCalls, SucceededCalls, FailedCalls                          int64
	PromptTokens, CompletionTokens, TotalTokens                      int64
	CachedTokens, CacheReadTokens, CacheWriteTokens, CacheMissTokens int64
	CacheReportedCalls, CacheHitCalls, CacheMissCalls                int64
	TokensReportedCalls                                              int64
	AverageDurationMS                                                sql.NullFloat64
}

type modelUsageByModelRow struct {
	ModelID, ModelName, Provider                       string
	ModelType                                          types.ModelType
	Calls, SucceededCalls                              int64
	FailedCalls, TotalTokens                           int64
	PromptTokens, CompletionTokens                     int64
	CacheReadTokens, CacheWriteTokens, CacheMissTokens int64
	CacheReportedCalls, CacheHitCalls                  int64
	TokensReportedCalls                                int64
	AverageDurationMS                                  sql.NullFloat64
}

func (r *modelUsageEventsRepository) Summary(ctx context.Context, tenantID uint64, filter types.ModelUsageFilter) (*types.ModelUsageSummary, error) {
	if tenantID == 0 {
		return nil, errors.New("tenant ID is required for model usage query")
	}
	base := applyModelUsageFilters(r.db.WithContext(ctx).Model(&types.ModelUsageEvent{}).Where("tenant_id = ?", tenantID), filter)
	var row modelUsageAggregateRow
	selectSQL := `COUNT(*) AS total_calls,
COALESCE(SUM(CASE WHEN success = TRUE THEN 1 ELSE 0 END), 0) AS succeeded_calls,
COALESCE(SUM(CASE WHEN success = FALSE THEN 1 ELSE 0 END), 0) AS failed_calls,
COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
COALESCE(SUM(total_tokens), 0) AS total_tokens,
COALESCE(SUM(cached_tokens), 0) AS cached_tokens,
COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens,
COALESCE(SUM(cache_miss_tokens), 0) AS cache_miss_tokens,
COALESCE(SUM(CASE WHEN cache_reported = TRUE THEN 1 ELSE 0 END), 0) AS cache_reported_calls,
COALESCE(SUM(CASE WHEN cache_reported = TRUE AND cache_status = 'hit' THEN 1 ELSE 0 END), 0) AS cache_hit_calls,
COALESCE(SUM(CASE WHEN cache_reported = TRUE AND cache_status = 'miss' THEN 1 ELSE 0 END), 0) AS cache_miss_calls,
COALESCE(SUM(CASE WHEN prompt_tokens IS NOT NULL OR completion_tokens IS NOT NULL OR total_tokens IS NOT NULL THEN 1 ELSE 0 END), 0) AS tokens_reported_calls,
AVG(duration_ms) AS average_duration_ms`
	if err := base.Select(selectSQL).Scan(&row).Error; err != nil {
		return nil, err
	}
	result := &types.ModelUsageSummary{
		TotalCalls: row.TotalCalls, SucceededCalls: row.SucceededCalls, FailedCalls: row.FailedCalls,
		PromptTokens: row.PromptTokens, CompletionTokens: row.CompletionTokens, TotalTokens: row.TotalTokens,
		CachedTokens: row.CachedTokens, CacheReadTokens: row.CacheReadTokens, CacheWriteTokens: row.CacheWriteTokens,
		CacheMissTokens: row.CacheMissTokens, CacheReportedCalls: row.CacheReportedCalls,
		CacheHitCalls: row.CacheHitCalls, CacheMissCalls: row.CacheMissCalls,
		TokensReportedCalls: row.TokensReportedCalls,
		CostStatus:          types.ModelUsageCostStatusUnavailable, ByModel: []types.ModelUsageByModel{},
	}
	if row.AverageDurationMS.Valid {
		result.AverageDurationMS = &row.AverageDurationMS.Float64
	}
	if row.CacheReportedCalls > 0 {
		rate := float64(row.CacheHitCalls) / float64(row.CacheReportedCalls)
		result.CacheHitRate = &rate
	}

	var cost struct {
		KnownCalls  int64
		AmountCalls int64
		Amount      sql.NullFloat64
		Currency    sql.NullString
		Currencies  int64
	}
	if err := base.Select("COUNT(CASE WHEN cost_amount IS NOT NULL AND NULLIF(cost_currency, '') IS NOT NULL THEN 1 END) AS known_calls, COUNT(cost_amount) AS amount_calls, SUM(CASE WHEN cost_amount IS NOT NULL AND NULLIF(cost_currency, '') IS NOT NULL THEN cost_amount END) AS amount, MAX(NULLIF(cost_currency, '')) AS currency, COUNT(DISTINCT NULLIF(cost_currency, '')) AS currencies").Scan(&cost).Error; err != nil {
		return nil, err
	}
	if cost.KnownCalls > 0 && cost.Amount.Valid && cost.Currencies == 1 && cost.Currency.Valid {
		result.CostAmount = &cost.Amount.Float64
		result.CostCurrency = cost.Currency.String
		if cost.KnownCalls == row.TotalCalls && cost.AmountCalls == row.TotalCalls {
			result.CostStatus = types.ModelUsageCostStatusAvailable
		} else {
			result.CostStatus = types.ModelUsageCostStatusPartial
		}
	} else if cost.AmountCalls > 0 {
		// Amounts without a currency, or across multiple currencies, cannot be
		// compared safely. Keep the partial status but leave the amount hidden.
		result.CostStatus = types.ModelUsageCostStatusPartial
	}

	var grouped []modelUsageByModelRow
	groupSelect := `model_id, model_name_snapshot AS model_name, model_type, provider,
COUNT(*) AS calls,
COALESCE(SUM(CASE WHEN success = TRUE THEN 1 ELSE 0 END), 0) AS succeeded_calls,
COALESCE(SUM(CASE WHEN success = FALSE THEN 1 ELSE 0 END), 0) AS failed_calls,
COALESCE(SUM(total_tokens), 0) AS total_tokens,
COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens,
COALESCE(SUM(cache_miss_tokens), 0) AS cache_miss_tokens,
COALESCE(SUM(CASE WHEN cache_reported = TRUE THEN 1 ELSE 0 END), 0) AS cache_reported_calls,
COALESCE(SUM(CASE WHEN cache_reported = TRUE AND cache_status = 'hit' THEN 1 ELSE 0 END), 0) AS cache_hit_calls,
COALESCE(SUM(CASE WHEN prompt_tokens IS NOT NULL OR completion_tokens IS NOT NULL OR total_tokens IS NOT NULL THEN 1 ELSE 0 END), 0) AS tokens_reported_calls,
AVG(duration_ms) AS average_duration_ms`
	if err := base.Select(groupSelect).Group("model_id, model_name_snapshot, model_type, provider").Order("calls DESC, model_id ASC").Scan(&grouped).Error; err != nil {
		return nil, err
	}
	for _, item := range grouped {
		entry := types.ModelUsageByModel{
			ModelID: item.ModelID, ModelName: item.ModelName, ModelType: item.ModelType, Provider: item.Provider,
			Calls: item.Calls, SucceededCalls: item.SucceededCalls, FailedCalls: item.FailedCalls,
			TotalTokens: item.TotalTokens, PromptTokens: item.PromptTokens, CompletionTokens: item.CompletionTokens,
			CacheReadTokens: item.CacheReadTokens, CacheWriteTokens: item.CacheWriteTokens, CacheMissTokens: item.CacheMissTokens,
			CacheReportedCalls: item.CacheReportedCalls, CacheHitCalls: item.CacheHitCalls,
			TokensReportedCalls: item.TokensReportedCalls,
			CostStatus:          types.ModelUsageCostStatusUnavailable,
		}
		if item.AverageDurationMS.Valid {
			entry.AverageDurationMS = &item.AverageDurationMS.Float64
		}
		if item.CacheReportedCalls > 0 {
			rate := float64(item.CacheHitCalls) / float64(item.CacheReportedCalls)
			entry.CacheHitRate = &rate
		}
		costQuery := applyModelUsageFilters(r.db.WithContext(ctx).Model(&types.ModelUsageEvent{}).
			Where("tenant_id = ? AND model_id = ? AND model_name_snapshot = ? AND model_type = ? AND provider = ?", tenantID, item.ModelID, item.ModelName, item.ModelType, item.Provider), filter)
		var groupedCost struct {
			KnownCalls  int64
			AmountCalls int64
			Amount      sql.NullFloat64
			Currency    sql.NullString
			Currencies  int64
		}
		if err := costQuery.Select("COUNT(CASE WHEN cost_amount IS NOT NULL AND NULLIF(cost_currency, '') IS NOT NULL THEN 1 END) AS known_calls, COUNT(cost_amount) AS amount_calls, SUM(CASE WHEN cost_amount IS NOT NULL AND NULLIF(cost_currency, '') IS NOT NULL THEN cost_amount END) AS amount, MAX(NULLIF(cost_currency, '')) AS currency, COUNT(DISTINCT NULLIF(cost_currency, '')) AS currencies").Scan(&groupedCost).Error; err != nil {
			return nil, err
		}
		if groupedCost.KnownCalls > 0 && groupedCost.Amount.Valid && groupedCost.Currencies == 1 && groupedCost.Currency.Valid {
			entry.CostAmount = &groupedCost.Amount.Float64
			entry.CostCurrency = groupedCost.Currency.String
			if groupedCost.KnownCalls == item.Calls && groupedCost.AmountCalls == item.Calls {
				entry.CostStatus = types.ModelUsageCostStatusAvailable
			} else {
				entry.CostStatus = types.ModelUsageCostStatusPartial
			}
		} else if groupedCost.AmountCalls > 0 {
			entry.CostStatus = types.ModelUsageCostStatusPartial
		}
		result.ByModel = append(result.ByModel, entry)
	}
	return result, nil
}

func applyModelUsageFilters(query *gorm.DB, filter types.ModelUsageFilter) *gorm.DB {
	if filter.StartedFrom != nil {
		query = query.Where("started_at >= ?", *filter.StartedFrom)
	}
	if filter.StartedTo != nil {
		query = query.Where("started_at < ?", *filter.StartedTo)
	}
	if filter.ModelID != "" {
		query = query.Where("model_id = ?", filter.ModelID)
	}
	if filter.ModelType != "" {
		query = query.Where("model_type = ?", filter.ModelType)
	}
	if filter.Provider != "" {
		query = query.Where("provider = ?", filter.Provider)
	}
	if filter.Operation != "" {
		query = query.Where("operation = ?", filter.Operation)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if filter.Success != nil {
		query = query.Where("success = ?", *filter.Success)
	}
	return query
}

func normalizeModelUsagePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

var _ interfaces.ModelUsageRepository = (*modelUsageEventsRepository)(nil)
