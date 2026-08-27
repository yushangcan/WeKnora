package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type evaluationCaseCountRow struct {
	RunID  string
	Status types.EvaluationRunStatus
	Count  int64
}

// ListRuns returns one tenant-scoped page without joining the one-to-many case table.
func (r *evaluationRepository) ListRuns(
	ctx context.Context,
	tenantID uint64,
	filter types.EvaluationRunListFilter,
) (*types.EvaluationRunPage, error) {
	query := applyEvaluationRunFilters(
		r.db.WithContext(ctx).Model(&types.EvaluationRunRecord{}).Where("tenant_id = ?", tenantID),
		filter,
	)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	records := make([]types.EvaluationRunRecord, 0, filter.PageSize)
	if err := query.
		Order("created_at DESC, run_id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}

	counts, err := r.evaluationCaseCounts(ctx, tenantID, evaluationRunIDs(records))
	if err != nil {
		return nil, err
	}
	items := make([]types.EvaluationRunSummary, 0, len(records))
	for i := range records {
		overview, err := evaluationOverviewFromRecord(&records[i], counts[records[i].RunID])
		if err != nil {
			return nil, fmt.Errorf("decode evaluation run %s: %w", records[i].RunID, err)
		}
		items = append(items, overview.Summary)
	}
	return &types.EvaluationRunPage{
		Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize,
	}, nil
}

// GetRunOverview loads one run and case counts without loading all case snapshots.
func (r *evaluationRepository) GetRunOverview(
	ctx context.Context,
	tenantID uint64,
	runID string,
) (*types.EvaluationRunOverview, error) {
	var record types.EvaluationRunRecord
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND run_id = ?", tenantID, runID).
		Take(&record).Error; err != nil {
		return nil, err
	}
	counts, err := r.evaluationCaseCounts(ctx, tenantID, []string{runID})
	if err != nil {
		return nil, err
	}
	return evaluationOverviewFromRecord(&record, counts[runID])
}

// GetRunOverviews batch-loads comparison candidates and their case counts.
func (r *evaluationRepository) GetRunOverviews(
	ctx context.Context,
	tenantID uint64,
	runIDs []string,
) ([]types.EvaluationRunOverview, error) {
	if len(runIDs) == 0 {
		return []types.EvaluationRunOverview{}, nil
	}
	var records []types.EvaluationRunRecord
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND run_id IN ?", tenantID, runIDs).
		Find(&records).Error; err != nil {
		return nil, err
	}
	counts, err := r.evaluationCaseCounts(ctx, tenantID, evaluationRunIDs(records))
	if err != nil {
		return nil, err
	}
	overviews := make([]types.EvaluationRunOverview, 0, len(records))
	for i := range records {
		overview, err := evaluationOverviewFromRecord(&records[i], counts[records[i].RunID])
		if err != nil {
			return nil, fmt.Errorf("decode evaluation run %s: %w", records[i].RunID, err)
		}
		overviews = append(overviews, *overview)
	}
	return overviews, nil
}

// ListRunCases returns a stable page of case snapshots for one tenant-scoped run.
func (r *evaluationRepository) ListRunCases(
	ctx context.Context,
	tenantID uint64,
	runID string,
	status types.EvaluationRunStatus,
	page int,
	pageSize int,
) (*types.EvaluationCasePage, error) {
	var exists int64
	if err := r.db.WithContext(ctx).Model(&types.EvaluationRunRecord{}).
		Where("tenant_id = ? AND run_id = ?", tenantID, runID).
		Count(&exists).Error; err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	query := r.db.WithContext(ctx).Model(&types.EvaluationRunCaseRecord{}).
		Where("tenant_id = ? AND run_id = ?", tenantID, runID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var records []types.EvaluationRunCaseRecord
	if err := query.Order("created_at ASC, case_id ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]types.EvaluationCaseResult, 0, len(records))
	for i := range records {
		var item types.EvaluationCaseResult
		if err := json.Unmarshal(records[i].ResultSnapshot, &item); err != nil {
			return nil, fmt.Errorf("decode evaluation case %s: %w", records[i].CaseID, err)
		}
		items = append(items, item)
	}
	return &types.EvaluationCasePage{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	}, nil
}

func applyEvaluationRunFilters(query *gorm.DB, filter types.EvaluationRunListFilter) *gorm.DB {
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.DatasetID != "" {
		query = query.Where("dataset_id = ?", filter.DatasetID)
	}
	if filter.ConfigHash != "" {
		query = query.Where("config_hash = ?", filter.ConfigHash)
	}
	if filter.EmbeddingModelID != "" {
		query = query.Where("embedding_model_id = ?", filter.EmbeddingModelID)
	}
	if filter.ChatModelID != "" {
		query = query.Where("chat_model_id = ?", filter.ChatModelID)
	}
	if filter.RerankModelID != "" {
		query = query.Where("rerank_model_id = ?", filter.RerankModelID)
	}
	if filter.StartedFrom != nil {
		query = query.Where("started_at >= ?", *filter.StartedFrom)
	}
	if filter.StartedTo != nil {
		query = query.Where("started_at <= ?", *filter.StartedTo)
	}
	return query
}

func (r *evaluationRepository) evaluationCaseCounts(
	ctx context.Context,
	tenantID uint64,
	runIDs []string,
) (map[string]types.EvaluationCaseStatusCounts, error) {
	counts := make(map[string]types.EvaluationCaseStatusCounts, len(runIDs))
	for _, runID := range runIDs {
		counts[runID] = types.EvaluationCaseStatusCounts{}
	}
	if len(runIDs) == 0 {
		return counts, nil
	}
	var rows []evaluationCaseCountRow
	if err := r.db.WithContext(ctx).Model(&types.EvaluationRunCaseRecord{}).
		Select("run_id, status, COUNT(*) AS count").
		Where("tenant_id = ? AND run_id IN ?", tenantID, runIDs).
		Group("run_id, status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		value := counts[row.RunID]
		value.Total += row.Count
		switch row.Status {
		case types.EvaluationRunStatusPending:
			value.Pending += row.Count
		case types.EvaluationRunStatusRunning:
			value.Running += row.Count
		case types.EvaluationRunStatusSuccess:
			value.Success += row.Count
		case types.EvaluationRunStatusPartial:
			value.Partial += row.Count
		case types.EvaluationRunStatusFailed:
			value.Failed += row.Count
		}
		counts[row.RunID] = value
	}
	return counts, nil
}

func evaluationRunIDs(records []types.EvaluationRunRecord) []string {
	ids := make([]string, 0, len(records))
	for i := range records {
		ids = append(ids, records[i].RunID)
	}
	return ids
}

func evaluationOverviewFromRecord(
	record *types.EvaluationRunRecord,
	counts types.EvaluationCaseStatusCounts,
) (*types.EvaluationRunOverview, error) {
	detail, err := evaluationDetailFromRecord(record)
	if err != nil {
		return nil, err
	}
	result := detail.Result
	status := record.Status
	if result != nil && result.Run.Status != "" && record.Status != types.EvaluationRunStatusFailed {
		status = result.Run.Status
	}
	return &types.EvaluationRunOverview{
		Summary: types.EvaluationRunSummary{
			RunID:                 record.RunID,
			Status:                status,
			Dataset:               detail.Config.Dataset,
			SourceKnowledgeBaseID: detail.Config.SourceKnowledgeBaseID,
			ConfigHash:            detail.Config.ConfigHash,
			ConfigSchemaVersion:   detail.Config.SchemaVersion,
			MetricVersion:         detail.Config.Runtime.MetricVersion,
			ResultVersion:         detail.Config.Runtime.ResultVersion,
			Models:                detail.Config.Models,
			Reproducibility:       detail.Config.Reproducibility,
			Progress: types.EvaluationRunProgress{
				Total: record.Total, Finished: record.Finished, Cases: counts,
			},
			Retrieval:    result.Retrieval,
			Answer:       result.Answer,
			Usage:        result.Usage,
			Cost:         result.Cost,
			Timing:       result.Timing,
			Warnings:     result.Warnings,
			ErrorMessage: record.ErrorMessage,
			StartedAt:    record.StartedAt,
			CompletedAt:  record.CompletedAt,
			CreatedAt:    record.CreatedAt,
			UpdatedAt:    record.UpdatedAt,
		},
		Config: detail.Config,
		Metric: detail.Metric,
	}, nil
}
