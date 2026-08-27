package service

import (
	"context"
	"strings"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

const (
	defaultEvaluationHistoryPageSize = 20
	maxEvaluationHistoryPageSize     = 100
	maxEvaluationComparisonRuns      = 5
)

// ListEvaluationRuns returns one validated history page for the current tenant.
func (e *EvaluationService) ListEvaluationRuns(
	ctx context.Context,
	filter types.EvaluationRunListFilter,
) (*types.EvaluationRunPage, error) {
	filter.Status = types.EvaluationRunStatus(strings.TrimSpace(string(filter.Status)))
	filter.DatasetID = strings.TrimSpace(filter.DatasetID)
	filter.ConfigHash = strings.TrimSpace(filter.ConfigHash)
	filter.EmbeddingModelID = strings.TrimSpace(filter.EmbeddingModelID)
	filter.ChatModelID = strings.TrimSpace(filter.ChatModelID)
	filter.RerankModelID = strings.TrimSpace(filter.RerankModelID)
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = defaultEvaluationHistoryPageSize
	}
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > maxEvaluationHistoryPageSize {
		return nil, apperrors.NewValidationError("invalid evaluation history pagination")
	}
	if filter.Status != "" && !isEvaluationRunStatus(filter.Status) {
		return nil, apperrors.NewValidationError("invalid evaluation run status")
	}
	if filter.StartedFrom != nil && filter.StartedTo != nil && filter.StartedFrom.After(*filter.StartedTo) {
		return nil, apperrors.NewValidationError("started_from must not be after started_to")
	}
	return e.evaluationRepository.ListRuns(ctx, types.MustTenantIDFromContext(ctx), filter)
}

// GetEvaluationRun returns one run overview without materializing all cases.
func (e *EvaluationService) GetEvaluationRun(
	ctx context.Context,
	runID string,
) (*types.EvaluationRunOverview, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, apperrors.NewValidationError("evaluation run ID is required")
	}
	return e.evaluationRepository.GetRunOverview(ctx, types.MustTenantIDFromContext(ctx), runID)
}

// ListEvaluationRunCases returns one validated page of case evidence.
func (e *EvaluationService) ListEvaluationRunCases(
	ctx context.Context,
	runID string,
	status types.EvaluationRunStatus,
	page int,
	pageSize int,
) (*types.EvaluationCasePage, error) {
	runID = strings.TrimSpace(runID)
	status = types.EvaluationRunStatus(strings.TrimSpace(string(status)))
	if runID == "" {
		return nil, apperrors.NewValidationError("evaluation run ID is required")
	}
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = defaultEvaluationHistoryPageSize
	}
	if page < 1 || pageSize < 1 || pageSize > maxEvaluationHistoryPageSize {
		return nil, apperrors.NewValidationError("invalid evaluation case pagination")
	}
	if status != "" && !isEvaluationRunStatus(status) {
		return nil, apperrors.NewValidationError("invalid evaluation case status")
	}
	return e.evaluationRepository.ListRunCases(
		ctx, types.MustTenantIDFromContext(ctx), runID, status, page, pageSize,
	)
}

// CompareEvaluationRuns compares immutable snapshots in the caller's requested order.
func (e *EvaluationService) CompareEvaluationRuns(
	ctx context.Context,
	baselineID string,
	runIDs []string,
) (*types.EvaluationComparison, error) {
	baselineID = strings.TrimSpace(baselineID)
	orderedIDs := uniqueEvaluationRunIDs(runIDs)
	if baselineID == "" {
		return nil, apperrors.NewValidationError("baseline_id is required")
	}
	if len(orderedIDs) < 2 || len(orderedIDs) > maxEvaluationComparisonRuns {
		return nil, apperrors.NewValidationError("comparison requires between 2 and 5 unique runs")
	}
	if !containsEvaluationRunID(orderedIDs, baselineID) {
		return nil, apperrors.NewValidationError("baseline_id must be included in run_ids")
	}

	overviews, err := e.evaluationRepository.GetRunOverviews(
		ctx, types.MustTenantIDFromContext(ctx), orderedIDs,
	)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]types.EvaluationRunOverview, len(overviews))
	for _, overview := range overviews {
		byID[overview.Summary.RunID] = overview
	}
	if len(byID) != len(orderedIDs) {
		return nil, gorm.ErrRecordNotFound
	}
	baseline := byID[baselineID]
	comparison := &types.EvaluationComparison{
		BaselineID: baselineID,
		Runs:       make([]types.EvaluationRunComparison, 0, len(orderedIDs)),
	}
	for _, runID := range orderedIDs {
		candidate := byID[runID]
		comparison.Runs = append(comparison.Runs, compareEvaluationOverview(baseline, candidate))
	}
	return comparison, nil
}

func compareEvaluationOverview(
	baseline types.EvaluationRunOverview,
	candidate types.EvaluationRunOverview,
) types.EvaluationRunComparison {
	qualityCompatibility := evaluationQualityCompatibility(baseline.Summary, candidate.Summary)
	costCompatibility := evaluationCostCompatibility(baseline.Summary, candidate.Summary)
	timingCompatibility := evaluationTimingCompatibility(baseline.Summary, candidate.Summary)
	result := types.EvaluationRunComparison{
		Run:                  candidate.Summary,
		Config:               candidate.Config,
		QualityCompatibility: qualityCompatibility,
		CostCompatibility:    costCompatibility,
		TimingCompatibility:  timingCompatibility,
	}
	if qualityCompatibility.Comparable {
		result.Quality = evaluationQualityDeltas(baseline.Summary, candidate.Summary)
	}
	if costCompatibility.Comparable {
		result.Cost.Amount = evaluationOptionalDelta(
			baseline.Summary.Cost.Amount, candidate.Summary.Cost.Amount,
		)
	}
	if timingCompatibility.Comparable {
		result.Cost.Calls = evaluationNumberDelta(
			float64(baseline.Summary.Usage.Calls.Total), float64(candidate.Summary.Usage.Calls.Total),
		)
		result.Cost.PromptTokens = evaluationNumberDelta(
			float64(baseline.Summary.Usage.Tokens.PromptTokens),
			float64(candidate.Summary.Usage.Tokens.PromptTokens),
		)
		result.Cost.CompletionTokens = evaluationNumberDelta(
			float64(baseline.Summary.Usage.Tokens.CompletionTokens),
			float64(candidate.Summary.Usage.Tokens.CompletionTokens),
		)
		result.Cost.TotalTokens = evaluationNumberDelta(
			float64(baseline.Summary.Usage.Tokens.TotalTokens),
			float64(candidate.Summary.Usage.Tokens.TotalTokens),
		)
		result.Cost.CachedTokens = evaluationNumberDelta(
			float64(baseline.Summary.Usage.Tokens.CachedTokens),
			float64(candidate.Summary.Usage.Tokens.CachedTokens),
		)
		result.Timing = evaluationTimingDeltas(baseline.Summary.Timing, candidate.Summary.Timing)
	}
	return result
}

func evaluationQualityCompatibility(
	baseline types.EvaluationRunSummary,
	candidate types.EvaluationRunSummary,
) types.EvaluationComparisonCompatibility {
	reasons := make([]string, 0)
	warnings := evaluationComparisonWarnings(baseline, candidate)
	if baseline.Dataset.ContentFingerprint == "" || candidate.Dataset.ContentFingerprint == "" {
		reasons = append(reasons, "dataset_fingerprint_unavailable")
	} else if baseline.Dataset.ContentFingerprint != candidate.Dataset.ContentFingerprint {
		reasons = append(reasons, "dataset_fingerprint_mismatch")
	}
	if baseline.MetricVersion == "" || candidate.MetricVersion == "" {
		reasons = append(reasons, "metric_version_unavailable")
	} else if baseline.MetricVersion != candidate.MetricVersion {
		reasons = append(reasons, "metric_version_mismatch")
	}
	if baseline.ResultVersion == "" || candidate.ResultVersion == "" {
		reasons = append(reasons, "result_version_unavailable")
	} else if baseline.ResultVersion != candidate.ResultVersion {
		reasons = append(reasons, "result_version_mismatch")
	}
	if !isTerminalEvaluationStatus(baseline.Status) || !isTerminalEvaluationStatus(candidate.Status) {
		reasons = append(reasons, "run_not_terminal")
	}
	if baseline.Retrieval == nil || baseline.Answer == nil || candidate.Retrieval == nil || candidate.Answer == nil {
		reasons = append(reasons, "quality_metrics_unavailable")
	}
	return types.EvaluationComparisonCompatibility{
		Comparable: len(reasons) == 0, Reasons: reasons, Warnings: warnings,
	}
}

func evaluationCostCompatibility(
	baseline types.EvaluationRunSummary,
	candidate types.EvaluationRunSummary,
) types.EvaluationComparisonCompatibility {
	reasons := make([]string, 0)
	warnings := evaluationComparisonWarnings(baseline, candidate)
	if baseline.Cost.Amount == nil || candidate.Cost.Amount == nil ||
		!isComparableEvaluationCostStatus(baseline.Cost.Status) ||
		!isComparableEvaluationCostStatus(candidate.Cost.Status) {
		reasons = append(reasons, "cost_unavailable")
	}
	if baseline.Cost.Currency == "" || candidate.Cost.Currency == "" {
		reasons = append(reasons, "cost_currency_unavailable")
	} else if baseline.Cost.Currency != candidate.Cost.Currency {
		reasons = append(reasons, "cost_currency_mismatch")
	}
	if baseline.Cost.PricingVersion == "" || candidate.Cost.PricingVersion == "" {
		reasons = append(reasons, "pricing_version_unavailable")
	} else if baseline.Cost.PricingVersion != candidate.Cost.PricingVersion {
		reasons = append(reasons, "pricing_version_mismatch")
	}
	if !isTerminalEvaluationStatus(baseline.Status) || !isTerminalEvaluationStatus(candidate.Status) {
		reasons = append(reasons, "run_not_terminal")
	}
	return types.EvaluationComparisonCompatibility{
		Comparable: len(reasons) == 0, Reasons: reasons, Warnings: warnings,
	}
}

func evaluationTimingCompatibility(
	baseline types.EvaluationRunSummary,
	candidate types.EvaluationRunSummary,
) types.EvaluationComparisonCompatibility {
	reasons := make([]string, 0)
	if !isTerminalEvaluationStatus(baseline.Status) || !isTerminalEvaluationStatus(candidate.Status) {
		reasons = append(reasons, "run_not_terminal")
	}
	warnings := evaluationComparisonWarnings(baseline, candidate)
	if len(reasons) == 0 {
		warnings = append(warnings, "timing_is_environment_dependent")
	}
	return types.EvaluationComparisonCompatibility{
		Comparable: len(reasons) == 0, Reasons: reasons, Warnings: warnings,
	}
}

func evaluationQualityDeltas(
	baseline types.EvaluationRunSummary,
	candidate types.EvaluationRunSummary,
) types.EvaluationQualityDeltas {
	return types.EvaluationQualityDeltas{
		Precision: evaluationNumberDelta(baseline.Retrieval.Precision, candidate.Retrieval.Precision),
		Recall:    evaluationNumberDelta(baseline.Retrieval.Recall, candidate.Retrieval.Recall),
		NDCG3:     evaluationNumberDelta(baseline.Retrieval.NDCG3, candidate.Retrieval.NDCG3),
		NDCG10:    evaluationNumberDelta(baseline.Retrieval.NDCG10, candidate.Retrieval.NDCG10),
		MRR:       evaluationNumberDelta(baseline.Retrieval.MRR, candidate.Retrieval.MRR),
		MAP:       evaluationNumberDelta(baseline.Retrieval.MAP, candidate.Retrieval.MAP),
		BLEU1:     evaluationNumberDelta(baseline.Answer.BLEU1, candidate.Answer.BLEU1),
		BLEU2:     evaluationNumberDelta(baseline.Answer.BLEU2, candidate.Answer.BLEU2),
		BLEU4:     evaluationNumberDelta(baseline.Answer.BLEU4, candidate.Answer.BLEU4),
		ROUGE1:    evaluationNumberDelta(baseline.Answer.ROUGE1, candidate.Answer.ROUGE1),
		ROUGE2:    evaluationNumberDelta(baseline.Answer.ROUGE2, candidate.Answer.ROUGE2),
		ROUGEL:    evaluationNumberDelta(baseline.Answer.ROUGEL, candidate.Answer.ROUGEL),
	}
}

func evaluationTimingDeltas(
	baseline types.EvaluationTimingResult,
	candidate types.EvaluationTimingResult,
) types.EvaluationTimingDeltas {
	return types.EvaluationTimingDeltas{
		TotalWallTimeMS:       evaluationNumberDelta(float64(baseline.TotalWallTimeMS), float64(candidate.TotalWallTimeMS)),
		PreparationMS:         evaluationNumberDelta(float64(baseline.PreparationMS), float64(candidate.PreparationMS)),
		EvaluationMS:          evaluationNumberDelta(float64(baseline.EvaluationMS), float64(candidate.EvaluationMS)),
		CleanupMS:             evaluationNumberDelta(float64(baseline.CleanupMS), float64(candidate.CleanupMS)),
		CaseAverageMS:         evaluationNumberDelta(float64(baseline.CaseAverageMS), float64(candidate.CaseAverageMS)),
		CaseP50MS:             evaluationNumberDelta(float64(baseline.CaseP50MS), float64(candidate.CaseP50MS)),
		CaseP95MS:             evaluationNumberDelta(float64(baseline.CaseP95MS), float64(candidate.CaseP95MS)),
		ModelCallCumulativeMS: evaluationNumberDelta(float64(baseline.ModelCallCumulativeMS), float64(candidate.ModelCallCumulativeMS)),
	}
}

func evaluationNumberDelta(baseline, value float64) types.EvaluationValueDelta {
	absolute := value - baseline
	result := types.EvaluationValueDelta{
		Baseline: float64Pointer(baseline),
		Value:    float64Pointer(value),
		Absolute: float64Pointer(absolute),
	}
	if baseline != 0 {
		result.Percent = float64Pointer(absolute / baseline * 100)
	}
	return result
}

func evaluationOptionalDelta(baseline, value *float64) types.EvaluationValueDelta {
	if baseline == nil || value == nil {
		return types.EvaluationValueDelta{}
	}
	return evaluationNumberDelta(*baseline, *value)
}

func evaluationComparisonWarnings(
	baseline types.EvaluationRunSummary,
	candidate types.EvaluationRunSummary,
) []string {
	warnings := make([]string, 0)
	if baseline.Status == types.EvaluationRunStatusPartial || candidate.Status == types.EvaluationRunStatusPartial {
		warnings = append(warnings, "partial_run")
	}
	return warnings
}

func isEvaluationRunStatus(status types.EvaluationRunStatus) bool {
	switch status {
	case types.EvaluationRunStatusPending, types.EvaluationRunStatusRunning,
		types.EvaluationRunStatusSuccess, types.EvaluationRunStatusPartial,
		types.EvaluationRunStatusFailed:
		return true
	default:
		return false
	}
}

func isTerminalEvaluationStatus(status types.EvaluationRunStatus) bool {
	return status == types.EvaluationRunStatusSuccess ||
		status == types.EvaluationRunStatusPartial ||
		status == types.EvaluationRunStatusFailed
}

func isComparableEvaluationCostStatus(status types.EvaluationCostStatus) bool {
	return status == types.EvaluationCostStatusComplete || status == types.EvaluationCostStatusPartial
}

func uniqueEvaluationRunIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsEvaluationRunID(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func float64Pointer(value float64) *float64 {
	copy := value
	return &copy
}
