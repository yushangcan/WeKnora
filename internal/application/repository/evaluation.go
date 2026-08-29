package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	evaluationobs "github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type evaluationRepository struct {
	db *gorm.DB
}

// NewEvaluationRepository creates a GORM-backed evaluation repository.
func NewEvaluationRepository(db *gorm.DB) interfaces.EvaluationRepository {
	return &evaluationRepository{db: db}
}

// CreateRun stores the immutable configuration and initial observable state.
func (r *evaluationRepository) CreateRun(
	ctx context.Context,
	detail *types.EvaluationDetail,
	temporaryKnowledgeBaseID string,
) error {
	record, err := newEvaluationRunRecord(detail, temporaryKnowledgeBaseID)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(record).Error
}

// GetRun loads one tenant-scoped run without consulting mutable model or knowledge-base rows.
func (r *evaluationRepository) GetRun(
	ctx context.Context,
	tenantID uint64,
	runID string,
) (*types.EvaluationDetail, error) {
	var record types.EvaluationRunRecord
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND run_id = ?", tenantID, runID).
		Take(&record).Error
	if err != nil {
		return nil, err
	}
	detail, err := evaluationDetailFromRecord(&record)
	if err != nil {
		return nil, err
	}
	cases, err := r.listEvaluationCases(ctx, tenantID, runID)
	if err != nil {
		return nil, err
	}
	detail.Result.Cases = cases
	detail.Result.Run.Status = normalizeEvaluationRunStatus(
		record.Status,
		detail.Result.Run.Status,
		record.Finished,
		len(cases),
	)
	return detail, nil
}

// UpdateRun persists a lifecycle transition or a run-level observation snapshot.
func (r *evaluationRepository) UpdateRun(ctx context.Context, detail *types.EvaluationDetail) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return updateEvaluationRun(tx, detail)
	})
}

// SaveCaseProgress atomically advances the run snapshot and upserts one completed case.
func (r *evaluationRepository) SaveCaseProgress(
	ctx context.Context,
	detail *types.EvaluationDetail,
	caseResult *types.EvaluationCaseResult,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := updateEvaluationRun(tx, detail); err != nil {
			return err
		}
		return upsertEvaluationCase(tx, detail.Task.TenantID, detail.Task.ID, caseResult)
	})
}

// SaveTerminalRun atomically stores the terminal run and every observed case.
func (r *evaluationRepository) SaveTerminalRun(
	ctx context.Context,
	detail *types.EvaluationDetail,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := updateEvaluationRun(tx, detail); err != nil {
			return err
		}
		if detail.Result == nil {
			return nil
		}
		for i := range detail.Result.Cases {
			if err := upsertEvaluationCase(
				tx,
				detail.Task.TenantID,
				detail.Task.ID,
				&detail.Result.Cases[i],
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// MarkInterruptedRunsFailed closes one tenant's non-terminal rows during an explicit recovery pass.
func (r *evaluationRepository) MarkInterruptedRunsFailed(
	ctx context.Context,
	tenantID uint64,
	completedAt time.Time,
	errorMessage string,
) (int64, error) {
	if tenantID == 0 {
		return 0, errors.New("tenant ID is required to recover interrupted evaluation runs")
	}
	if completedAt.IsZero() {
		completedAt = time.Now()
	}
	if errorMessage == "" {
		errorMessage = "evaluation process interrupted before completion"
	}
	errorMessage = evaluationobs.SafeErrorText(errorMessage)
	result := r.db.WithContext(ctx).
		Model(&types.EvaluationRunRecord{}).
		Where("tenant_id = ? AND status IN ?", tenantID, []types.EvaluationRunStatus{
			types.EvaluationRunStatusPending,
			types.EvaluationRunStatusRunning,
		}).
		Updates(map[string]interface{}{
			"status":        types.EvaluationRunStatusFailed,
			"error_message": errorMessage,
			"completed_at":  completedAt,
			"updated_at":    completedAt,
			"revision":      gorm.Expr("revision + 1"),
		})
	return result.RowsAffected, result.Error
}

func newEvaluationRunRecord(
	detail *types.EvaluationDetail,
	temporaryKnowledgeBaseID string,
) (*types.EvaluationRunRecord, error) {
	if err := validateEvaluationDetail(detail); err != nil {
		return nil, err
	}
	configSnapshot, err := marshalEvaluationSnapshot(detail.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal evaluation config: %w", err)
	}
	paramsSnapshot, err := marshalEvaluationSnapshot(evaluationobs.SafeParamsSnapshot(detail.Params))
	if err != nil {
		return nil, fmt.Errorf("marshal evaluation params: %w", err)
	}
	metricSnapshot, err := marshalEvaluationSnapshot(detail.Metric)
	if err != nil {
		return nil, fmt.Errorf("marshal evaluation metric: %w", err)
	}
	resultSnapshot, err := marshalEvaluationRunResultSnapshot(detail.Result)
	if err != nil {
		return nil, fmt.Errorf("marshal evaluation result: %w", err)
	}

	rerankModelID := ""
	if detail.Config.Models.Rerank != nil {
		rerankModelID = detail.Config.Models.Rerank.ID
	}
	now := time.Now()
	return &types.EvaluationRunRecord{
		RunID:                    detail.Task.ID,
		TenantID:                 detail.Task.TenantID,
		SourceKnowledgeBaseID:    detail.Config.SourceKnowledgeBaseID,
		TemporaryKnowledgeBaseID: temporaryKnowledgeBaseID,
		DatasetID:                detail.Config.Dataset.ID,
		DatasetVersion:           detail.Config.Dataset.Version,
		DatasetFingerprint:       detail.Config.Dataset.ContentFingerprint,
		ConfigHash:               detail.Config.ConfigHash,
		EmbeddingModelID:         detail.Config.Models.Embedding.ID,
		ChatModelID:              detail.Config.Models.Chat.ID,
		RerankModelID:            rerankModelID,
		Status:                   persistentEvaluationStatus(detail),
		Total:                    detail.Task.Total,
		Finished:                 detail.Task.Finished,
		ErrorMessage:             evaluationobs.SafeErrorText(detail.Task.ErrMsg),
		ConfigSnapshot:           configSnapshot,
		ParamsSnapshot:           paramsSnapshot,
		MetricSnapshot:           metricSnapshot,
		ResultSnapshot:           resultSnapshot,
		StartedAt:                detail.Task.StartTime,
		CompletedAt:              evaluationCompletedAt(detail),
		CreatedAt:                now,
		UpdatedAt:                now,
		Revision:                 1,
	}, nil
}

func updateEvaluationRun(tx *gorm.DB, detail *types.EvaluationDetail) error {
	if err := validateEvaluationDetail(detail); err != nil {
		return err
	}
	metricSnapshot, err := marshalEvaluationSnapshot(detail.Metric)
	if err != nil {
		return fmt.Errorf("marshal evaluation metric: %w", err)
	}
	resultSnapshot, err := marshalEvaluationRunResultSnapshot(detail.Result)
	if err != nil {
		return fmt.Errorf("marshal evaluation result: %w", err)
	}

	result := tx.Model(&types.EvaluationRunRecord{}).
		Where("tenant_id = ? AND run_id = ?", detail.Task.TenantID, detail.Task.ID).
		Updates(map[string]interface{}{
			"status":          persistentEvaluationStatus(detail),
			"total":           detail.Task.Total,
			"finished":        detail.Task.Finished,
			"error_message":   evaluationobs.SafeErrorText(detail.Task.ErrMsg),
			"metric_snapshot": metricSnapshot,
			"result_snapshot": resultSnapshot,
			"completed_at":    evaluationCompletedAt(detail),
			"updated_at":      time.Now(),
			"revision":        gorm.Expr("revision + 1"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func upsertEvaluationCase(
	tx *gorm.DB,
	tenantID uint64,
	runID string,
	caseResult *types.EvaluationCaseResult,
) error {
	if caseResult == nil || caseResult.CaseID == "" {
		return errors.New("evaluation case result is incomplete")
	}
	usageSnapshot, err := marshalEvaluationSnapshot(caseResult.Usage)
	if err != nil {
		return fmt.Errorf("marshal evaluation case usage: %w", err)
	}
	warningsSnapshot, err := marshalEvaluationSnapshot(caseResult.Warnings)
	if err != nil {
		return fmt.Errorf("marshal evaluation case warnings: %w", err)
	}
	resultSnapshot, err := marshalEvaluationSnapshot(caseResult)
	if err != nil {
		return fmt.Errorf("marshal evaluation case result: %w", err)
	}
	now := time.Now()
	record := &types.EvaluationRunCaseRecord{
		RunID:            runID,
		CaseID:           caseResult.CaseID,
		TenantID:         tenantID,
		Status:           caseResult.Status,
		StartedAt:        caseResult.StartedAt,
		CompletedAt:      caseResult.CompletedAt,
		DurationMS:       caseResult.DurationMS,
		UsageSnapshot:    usageSnapshot,
		WarningsSnapshot: warningsSnapshot,
		ResultSnapshot:   resultSnapshot,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "run_id"}, {Name: "case_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"status":            record.Status,
			"started_at":        record.StartedAt,
			"completed_at":      record.CompletedAt,
			"duration_ms":       record.DurationMS,
			"usage_snapshot":    record.UsageSnapshot,
			"warnings_snapshot": record.WarningsSnapshot,
			"result_snapshot":   record.ResultSnapshot,
			"updated_at":        record.UpdatedAt,
		}),
	}).Create(record).Error
}

func evaluationDetailFromRecord(record *types.EvaluationRunRecord) (*types.EvaluationDetail, error) {
	if record == nil {
		return nil, errors.New("evaluation run record is nil")
	}
	var config types.EvaluationRunConfig
	if err := unmarshalEvaluationSnapshot(record.ConfigSnapshot, &config); err != nil {
		return nil, fmt.Errorf("decode evaluation config: %w", err)
	}
	var params types.ChatManage
	if err := unmarshalEvaluationSnapshot(record.ParamsSnapshot, &params); err != nil {
		return nil, fmt.Errorf("decode evaluation params: %w", err)
	}
	var metric *types.MetricResult
	if hasEvaluationSnapshot(record.MetricSnapshot) {
		metric = &types.MetricResult{}
		if err := unmarshalEvaluationSnapshot(record.MetricSnapshot, metric); err != nil {
			return nil, fmt.Errorf("decode evaluation metric: %w", err)
		}
	}
	var runResult types.EvaluationRunResult
	if err := unmarshalEvaluationSnapshot(record.ResultSnapshot, &runResult); err != nil {
		return nil, fmt.Errorf("decode evaluation result: %w", err)
	}

	runResult.Run.RunID = record.RunID
	runResult.Run.TenantID = record.TenantID
	runResult.Run.DatasetID = record.DatasetID
	runResult.Run.StartedAt = record.StartedAt
	runResult.Run.CompletedAt = record.CompletedAt
	runResult.Run.Status = normalizeEvaluationRunStatus(
		record.Status,
		runResult.Run.Status,
		record.Finished,
		len(runResult.Cases),
	)

	return &types.EvaluationDetail{
		Task: &types.EvaluationTask{
			ID:        record.RunID,
			TenantID:  record.TenantID,
			DatasetID: record.DatasetID,
			StartTime: record.StartedAt,
			Status:    legacyEvaluationStatus(record.Status),
			ErrMsg:    record.ErrorMessage,
			Total:     record.Total,
			Finished:  record.Finished,
		},
		Params: &params,
		Config: &config,
		Metric: metric,
		Result: &runResult,
	}, nil
}

// normalizeEvaluationRunStatus applies the persisted lifecycle status while
// retaining partial-result meaning for failed runs with observed progress.
func normalizeEvaluationRunStatus(
	recordStatus types.EvaluationRunStatus,
	snapshotStatus types.EvaluationRunStatus,
	finished int,
	caseCount int,
) types.EvaluationRunStatus {
	if recordStatus != types.EvaluationRunStatusFailed {
		return recordStatus
	}
	if snapshotStatus == types.EvaluationRunStatusPartial {
		return types.EvaluationRunStatusPartial
	}
	if snapshotStatus == types.EvaluationRunStatusFailed {
		return types.EvaluationRunStatusFailed
	}
	if finished > 0 || caseCount > 0 {
		return types.EvaluationRunStatusPartial
	}
	return types.EvaluationRunStatusFailed
}

func (r *evaluationRepository) listEvaluationCases(
	ctx context.Context,
	tenantID uint64,
	runID string,
) ([]types.EvaluationCaseResult, error) {
	var records []types.EvaluationRunCaseRecord
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND run_id = ?", tenantID, runID).
		Find(&records).Error; err != nil {
		return nil, err
	}
	cases := make([]types.EvaluationCaseResult, 0, len(records))
	for i := range records {
		var result types.EvaluationCaseResult
		if err := unmarshalEvaluationSnapshot(records[i].ResultSnapshot, &result); err != nil {
			return nil, fmt.Errorf("decode evaluation case %s: %w", records[i].CaseID, err)
		}
		cases = append(cases, result)
	}
	sort.Slice(cases, func(i, j int) bool {
		left, leftErr := strconv.Atoi(cases[i].CaseID)
		right, rightErr := strconv.Atoi(cases[j].CaseID)
		if leftErr == nil && rightErr == nil && left != right {
			return left < right
		}
		return cases[i].CaseID < cases[j].CaseID
	})
	return cases, nil
}

func validateEvaluationDetail(detail *types.EvaluationDetail) error {
	if detail == nil || detail.Task == nil {
		return errors.New("evaluation detail task is required")
	}
	if detail.Task.ID == "" || detail.Task.TenantID == 0 {
		return errors.New("evaluation run ID and tenant ID are required")
	}
	if detail.Config == nil || detail.Params == nil || detail.Result == nil {
		return errors.New("evaluation config, params and result are required")
	}
	return nil
}

func marshalEvaluationSnapshot(value interface{}) (types.JSON, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return types.JSON(data), nil
}

func marshalEvaluationRunResultSnapshot(result *types.EvaluationRunResult) (types.JSON, error) {
	if result == nil {
		return nil, nil
	}
	copy := *result
	copy.Cases = []types.EvaluationCaseResult{}
	return marshalEvaluationSnapshot(&copy)
}

func unmarshalEvaluationSnapshot(snapshot types.JSON, target interface{}) error {
	if !hasEvaluationSnapshot(snapshot) {
		return errors.New("evaluation snapshot is empty")
	}
	return json.Unmarshal(snapshot, target)
}

func hasEvaluationSnapshot(snapshot types.JSON) bool {
	return len(snapshot) > 0 && string(snapshot) != "null"
}

func evaluationCompletedAt(detail *types.EvaluationDetail) *time.Time {
	if detail == nil || detail.Result == nil || detail.Result.Run.CompletedAt == nil {
		return nil
	}
	completedAt := *detail.Result.Run.CompletedAt
	return &completedAt
}

func persistentEvaluationStatus(detail *types.EvaluationDetail) types.EvaluationRunStatus {
	if detail != nil && detail.Result != nil && detail.Result.Run.Status == types.EvaluationRunStatusPartial {
		return types.EvaluationRunStatusPartial
	}
	if detail == nil || detail.Task == nil {
		return types.EvaluationRunStatusPending
	}
	switch detail.Task.Status {
	case types.EvaluationStatueRunning:
		return types.EvaluationRunStatusRunning
	case types.EvaluationStatueSuccess:
		return types.EvaluationRunStatusSuccess
	case types.EvaluationStatueFailed:
		return types.EvaluationRunStatusFailed
	default:
		return types.EvaluationRunStatusPending
	}
}

func legacyEvaluationStatus(status types.EvaluationRunStatus) types.EvaluationStatue {
	switch status {
	case types.EvaluationRunStatusRunning:
		return types.EvaluationStatueRunning
	case types.EvaluationRunStatusSuccess:
		return types.EvaluationStatueSuccess
	case types.EvaluationRunStatusFailed, types.EvaluationRunStatusPartial:
		return types.EvaluationStatueFailed
	default:
		return types.EvaluationStatuePending
	}
}
