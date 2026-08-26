package evaluation

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type caseObservation struct {
	caseID      string
	status      types.EvaluationRunStatus
	startedAt   time.Time
	completedAt *time.Time
	duration    time.Duration
	evidence    types.EvaluationCaseEvidence
	warnings    []types.EvaluationWarning
}

// Observer owns all in-memory observations for exactly one evaluation run.
type Observer struct {
	runID     string
	tenantID  uint64
	datasetID string
	startedAt time.Time

	collector *ModelCallCollector

	mu             sync.RWMutex
	completedAt    *time.Time
	phaseDurations map[types.EvaluationPhase]time.Duration
	cases          map[string]caseObservation
	warnings       []types.EvaluationWarning
}

func NewObserver(runID string, tenantID uint64, datasetID string, startedAt time.Time) *Observer {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &Observer{
		runID:          runID,
		tenantID:       tenantID,
		datasetID:      datasetID,
		startedAt:      startedAt,
		collector:      NewModelCallCollector(),
		phaseDurations: make(map[types.EvaluationPhase]time.Duration),
		cases:          make(map[string]caseObservation),
	}
}

func (o *Observer) RunID() string {
	if o == nil {
		return ""
	}
	return o.runID
}

func (o *Observer) CollectorSnapshot() []ModelCallRecord {
	if o == nil {
		return nil
	}
	return o.collector.Snapshot()
}

// StartPhase returns a derived phase context and an idempotent completion
// function. Several case goroutines may use the derived context concurrently.
func (o *Observer) StartPhase(ctx context.Context, phase types.EvaluationPhase) (context.Context, func()) {
	phaseCtx := WithEvaluationPhase(ctx, phase)
	startedAt := time.Now()
	var once sync.Once
	return phaseCtx, func() {
		once.Do(func() {
			if o == nil {
				return
			}
			o.mu.Lock()
			o.phaseDurations[phase] += time.Since(startedAt)
			o.mu.Unlock()
		})
	}
}

// StartCase derives a unique immutable case context and records its wall time.
func (o *Observer) StartCase(ctx context.Context, caseID string) (context.Context, func(error)) {
	caseCtx := WithEvaluationCase(WithEvaluationPhase(ctx, types.EvaluationPhaseEvaluation), caseID)
	startedAt := time.Now()
	if o != nil {
		o.mu.Lock()
		o.cases[caseID] = caseObservation{
			caseID:    caseID,
			status:    types.EvaluationRunStatusRunning,
			startedAt: startedAt,
		}
		o.mu.Unlock()
	}
	var once sync.Once
	return caseCtx, func(caseErr error) {
		once.Do(func() {
			if o == nil {
				return
			}
			completedAt := time.Now()
			status := types.EvaluationRunStatusSuccess
			var warnings []types.EvaluationWarning
			if caseErr != nil {
				status = types.EvaluationRunStatusFailed
				warnings = append(warnings, types.EvaluationWarning{
					Code:    "case_failed",
					Message: "The case did not complete successfully; only observed data is included.",
				})
			}
			o.mu.Lock()
			existing := o.cases[caseID]
			o.cases[caseID] = caseObservation{
				caseID:      caseID,
				status:      status,
				startedAt:   startedAt,
				completedAt: &completedAt,
				duration:    completedAt.Sub(startedAt),
				evidence:    existing.evidence,
				warnings:    warnings,
			}
			o.mu.Unlock()
		})
	}
}

func (o *Observer) AddWarning(code, message string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.warnings = append(o.warnings, types.EvaluationWarning{Code: code, Message: message})
	o.mu.Unlock()
}

// RecordCaseEvidence attaches non-text audit evidence to an observed case.
func (o *Observer) RecordCaseEvidence(caseID string, evidence types.EvaluationCaseEvidence) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	observedCase, ok := o.cases[caseID]
	if !ok {
		return
	}
	observedCase.evidence = cloneCaseEvidence(evidence)
	if evidence.UnmappedResultCount > 0 {
		observedCase.warnings = appendWarningOnce(observedCase.warnings, types.EvaluationWarning{
			Code:    "unmapped_retrieval_result",
			Message: "One or more ranked retrieval results lacked a valid evaluation passage ID and were retained as non-relevant placeholders.",
		})
		o.warnings = appendWarningOnce(o.warnings, types.EvaluationWarning{
			Code:    "unmapped_retrieval_results",
			Message: "One or more evaluation cases contained ranked retrieval results without valid passage IDs.",
		})
	}
	o.cases[caseID] = observedCase
}

// MarkCaseFailure records a post-pipeline failure stage for terminal persistence.
func (o *Observer) MarkCaseFailure(caseID, stage string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	observedCase, ok := o.cases[caseID]
	if !ok {
		return
	}
	observedCase.status = types.EvaluationRunStatusFailed
	observedCase.evidence.FailureStage = stage
	observedCase.warnings = appendWarningOnce(observedCase.warnings, types.EvaluationWarning{
		Code:    "case_persistence_failed",
		Message: "The case completed but its progress snapshot could not be persisted.",
	})
	o.cases[caseID] = observedCase
}

func (o *Observer) Complete() {
	if o == nil {
		return
	}
	completedAt := time.Now()
	o.mu.Lock()
	if o.completedAt == nil {
		o.completedAt = &completedAt
	}
	o.mu.Unlock()
}

// Snapshot builds a detached API result without changing metric formulas.
func (o *Observer) Snapshot(taskStatus types.EvaluationStatue, metric *types.MetricResult) *types.EvaluationRunResult {
	if o == nil {
		return nil
	}
	records := o.collector.Snapshot()

	o.mu.RLock()
	completedAt := cloneTime(o.completedAt)
	phaseDurations := make(map[types.EvaluationPhase]time.Duration, len(o.phaseDurations))
	for phase, duration := range o.phaseDurations {
		phaseDurations[phase] = duration
	}
	cases := make([]caseObservation, 0, len(o.cases))
	for _, observedCase := range o.cases {
		observedCase.completedAt = cloneTime(observedCase.completedAt)
		observedCase.evidence = cloneCaseEvidence(observedCase.evidence)
		observedCase.warnings = append([]types.EvaluationWarning{}, observedCase.warnings...)
		cases = append(cases, observedCase)
	}
	warnings := append([]types.EvaluationWarning{}, o.warnings...)
	o.mu.RUnlock()

	usage := aggregateUsage(records)
	caseResults := make([]types.EvaluationCaseResult, 0, len(cases))
	caseDurations := make([]int64, 0, len(cases))
	sort.Slice(cases, func(i, j int) bool { return cases[i].caseID < cases[j].caseID })
	for _, observedCase := range cases {
		durationMS := observedCase.duration.Milliseconds()
		if observedCase.completedAt == nil {
			durationMS = time.Since(observedCase.startedAt).Milliseconds()
		}
		caseDurations = append(caseDurations, durationMS)
		caseResults = append(caseResults, types.EvaluationCaseResult{
			CaseID:      observedCase.caseID,
			Status:      observedCase.status,
			StartedAt:   observedCase.startedAt,
			CompletedAt: cloneTime(observedCase.completedAt),
			DurationMS:  durationMS,
			Usage:       aggregateUsage(filterRecordsByCase(records, observedCase.caseID)),
			Evidence:    cloneCaseEvidence(observedCase.evidence),
			Warnings:    append([]types.EvaluationWarning{}, observedCase.warnings...),
		})
	}

	if usage.UnavailableCallCount > 0 {
		warnings = append(warnings, types.EvaluationWarning{
			Code:    "usage_incomplete",
			Message: "One or more model calls did not report token usage.",
		})
	}
	cost := types.EvaluationCostResult{
		Status:   types.EvaluationCostStatusNotApplicable,
		Source:   "not_reported",
		Amount:   nil,
		Warnings: []types.EvaluationWarning{},
	}
	if usage.Calls.Total > 0 {
		cost.Status = types.EvaluationCostStatusUnavailable
		cost.Warnings = append(cost.Warnings, types.EvaluationWarning{
			Code:    "pricing_not_available",
			Message: "Stage one does not calculate prices; token usage and call counts are reported when available.",
		})
	}

	result := &types.EvaluationRunResult{
		SchemaVersion: types.EvaluationResultSchemaVersion,
		Run: types.EvaluationRunMetadata{
			RunID:       o.runID,
			TenantID:    o.tenantID,
			DatasetID:   o.datasetID,
			StartedAt:   o.startedAt,
			CompletedAt: completedAt,
			Status:      runStatus(taskStatus, metric != nil || len(records) > 0 || len(cases) > 0),
		},
		Retrieval: qualityRetrieval(metric),
		Answer:    qualityAnswer(metric),
		Usage:     usage,
		Cost:      cost,
		Timing:    timingResult(o.startedAt, completedAt, phaseDurations, caseDurations, usage),
		Cases:     caseResults,
		Warnings:  warnings,
	}
	return result
}

func cloneCaseEvidence(source types.EvaluationCaseEvidence) types.EvaluationCaseEvidence {
	result := source
	result.GroundTruthPIDs = append([]int(nil), source.GroundTruthPIDs...)
	result.SearchPIDs = append([]int(nil), source.SearchPIDs...)
	result.RerankPIDs = append([]int(nil), source.RerankPIDs...)
	result.MetricInputPIDs = append([]int(nil), source.MetricInputPIDs...)
	if source.Metrics != nil {
		metrics := *source.Metrics
		result.Metrics = &metrics
	}
	return result
}

func appendWarningOnce(
	warnings []types.EvaluationWarning,
	warning types.EvaluationWarning,
) []types.EvaluationWarning {
	for _, existing := range warnings {
		if existing.Code == warning.Code {
			return warnings
		}
	}
	return append(warnings, warning)
}

func qualityRetrieval(metric *types.MetricResult) *types.EvaluationRetrievalResult {
	if metric == nil {
		return nil
	}
	m := metric.RetrievalMetrics
	return &types.EvaluationRetrievalResult{
		Precision: m.Precision,
		Recall:    m.Recall,
		NDCG3:     m.NDCG3,
		NDCG10:    m.NDCG10,
		MRR:       m.MRR,
		MAP:       m.MAP,
	}
}

func qualityAnswer(metric *types.MetricResult) *types.EvaluationAnswerResult {
	if metric == nil {
		return nil
	}
	m := metric.GenerationMetrics
	return &types.EvaluationAnswerResult{
		BLEU1:  m.BLEU1,
		BLEU2:  m.BLEU2,
		BLEU4:  m.BLEU4,
		ROUGE1: m.ROUGE1,
		ROUGE2: m.ROUGE2,
		ROUGEL: m.ROUGEL,
	}
}

func runStatus(status types.EvaluationStatue, hasPartialData bool) types.EvaluationRunStatus {
	switch status {
	case types.EvaluationStatuePending:
		return types.EvaluationRunStatusPending
	case types.EvaluationStatueRunning:
		return types.EvaluationRunStatusRunning
	case types.EvaluationStatueSuccess:
		return types.EvaluationRunStatusSuccess
	case types.EvaluationStatueFailed:
		if hasPartialData {
			return types.EvaluationRunStatusPartial
		}
		return types.EvaluationRunStatusFailed
	default:
		return types.EvaluationRunStatusFailed
	}
}

func timingResult(
	startedAt time.Time,
	completedAt *time.Time,
	phases map[types.EvaluationPhase]time.Duration,
	caseDurations []int64,
	usage types.EvaluationUsageResult,
) types.EvaluationTimingResult {
	end := time.Now()
	if completedAt != nil {
		end = *completedAt
	}
	result := types.EvaluationTimingResult{
		TotalWallTimeMS:       end.Sub(startedAt).Milliseconds(),
		PreparationMS:         phases[types.EvaluationPhasePreparation].Milliseconds(),
		EvaluationMS:          phases[types.EvaluationPhaseEvaluation].Milliseconds(),
		CleanupMS:             phases[types.EvaluationPhaseCleanup].Milliseconds(),
		CaseCount:             len(caseDurations),
		ModelCallCumulativeMS: modelDurationMS(usage),
	}
	if len(caseDurations) == 0 {
		return result
	}
	sorted := append([]int64(nil), caseDurations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum int64
	for _, duration := range sorted {
		sum += duration
	}
	result.CaseAverageMS = sum / int64(len(sorted))
	result.CaseMinimumMS = sorted[0]
	result.CaseMaximumMS = sorted[len(sorted)-1]
	result.CaseP50MS = percentile(sorted, 0.50)
	result.CaseP95MS = percentile(sorted, 0.95)
	return result
}

func aggregateUsage(records []ModelCallRecord) types.EvaluationUsageResult {
	result := types.EvaluationUsageResult{
		Status:      types.EvaluationUsageStatusNotApplicable,
		CacheStatus: "not_applicable",
		ByModel:     []types.EvaluationModelUsage{},
		ByPhase:     []types.EvaluationPhaseUsage{},
	}
	if len(records) == 0 {
		return result
	}

	type modelKey struct {
		modelType types.EvaluationModelType
		modelID   string
		modelName string
		operation types.EvaluationModelOperation
	}
	modelGroups := make(map[modelKey][]ModelCallRecord)
	phaseGroups := make(map[types.EvaluationPhase][]ModelCallRecord)
	cacheStatuses := make(map[string]struct{})
	hasChatCalls := false
	for _, record := range records {
		addRecordToUsage(&result.Calls, &result.Tokens, record)
		if record.UsageSource == types.EvaluationUsageSourceProviderReported {
			result.ReportedCallCount += record.CallCount
		} else {
			result.UnavailableCallCount += record.CallCount
		}
		if record.ModelType == types.EvaluationModelTypeChat {
			hasChatCalls = true
			if record.Usage != nil {
				status := string(record.Usage.CacheStatus)
				if status == "" {
					status = "unreported"
				}
				cacheStatuses[status] = struct{}{}
			}
		}
		key := modelKey{record.ModelType, record.ModelID, record.ModelName, record.Operation}
		modelGroups[key] = append(modelGroups[key], record)
		phaseGroups[record.Phase] = append(phaseGroups[record.Phase], record)
	}
	result.Status = usageStatus(result.Calls.Total, result.ReportedCallCount, result.UnavailableCallCount)
	result.CacheStatus = combinedCacheStatus(cacheStatuses, hasChatCalls)

	modelKeys := make([]modelKey, 0, len(modelGroups))
	for key := range modelGroups {
		modelKeys = append(modelKeys, key)
	}
	sort.Slice(modelKeys, func(i, j int) bool {
		if modelKeys[i].modelType != modelKeys[j].modelType {
			return modelKeys[i].modelType < modelKeys[j].modelType
		}
		if modelKeys[i].modelID != modelKeys[j].modelID {
			return modelKeys[i].modelID < modelKeys[j].modelID
		}
		return modelKeys[i].operation < modelKeys[j].operation
	})
	for _, key := range modelKeys {
		calls, tokens, duration, source := summarizeRecords(modelGroups[key])
		result.ByModel = append(result.ByModel, types.EvaluationModelUsage{
			ModelType:   key.modelType,
			ModelID:     key.modelID,
			ModelName:   key.modelName,
			Operation:   key.operation,
			UsageSource: source,
			Calls:       calls,
			Tokens:      tokens,
			DurationMS:  duration,
		})
	}

	phases := make([]types.EvaluationPhase, 0, len(phaseGroups))
	for phase := range phaseGroups {
		phases = append(phases, phase)
	}
	sort.Slice(phases, func(i, j int) bool { return phases[i] < phases[j] })
	for _, phase := range phases {
		calls, tokens, duration, source := summarizeRecords(phaseGroups[phase])
		result.ByPhase = append(result.ByPhase, types.EvaluationPhaseUsage{
			Phase:       phase,
			UsageSource: source,
			Calls:       calls,
			Tokens:      tokens,
			DurationMS:  duration,
		})
	}
	return result
}

func summarizeRecords(records []ModelCallRecord) (
	types.EvaluationCallCounts,
	types.EvaluationTokenTotals,
	int64,
	types.EvaluationUsageSource,
) {
	var calls types.EvaluationCallCounts
	var tokens types.EvaluationTokenTotals
	var duration int64
	reported := 0
	for _, record := range records {
		addRecordToUsage(&calls, &tokens, record)
		duration += record.DurationMS
		if record.UsageSource == types.EvaluationUsageSourceProviderReported {
			reported += record.CallCount
		}
	}
	source := types.EvaluationUsageSourceUnavailable
	if calls.Total > 0 && reported == calls.Total {
		source = types.EvaluationUsageSourceProviderReported
	}
	return calls, tokens, duration, source
}

func addRecordToUsage(calls *types.EvaluationCallCounts, tokens *types.EvaluationTokenTotals, record ModelCallRecord) {
	callCount := record.CallCount
	if callCount <= 0 {
		callCount = 1
	}
	calls.Total += callCount
	if record.Success {
		calls.Succeeded += callCount
	} else {
		calls.Failed += callCount
	}
	calls.Items += record.ItemCount
	if record.UsageSource != types.EvaluationUsageSourceProviderReported || record.Usage == nil {
		return
	}
	tokens.PromptTokens += record.Usage.PromptTokens
	tokens.CompletionTokens += record.Usage.CompletionTokens
	tokens.TotalTokens += record.Usage.TotalTokens
	tokens.CachedTokens += record.Usage.CachedTokens
	tokens.CacheReadTokens += record.Usage.CacheReadTokens
	tokens.CacheWriteTokens += record.Usage.CacheWriteTokens
	tokens.CacheMissTokens += record.Usage.CacheMissTokens
}

func usageStatus(total, reported, unavailable int) types.EvaluationUsageStatus {
	switch {
	case total == 0:
		return types.EvaluationUsageStatusNotApplicable
	case reported == total:
		return types.EvaluationUsageStatusComplete
	case unavailable == total:
		return types.EvaluationUsageStatusUnavailable
	default:
		return types.EvaluationUsageStatusPartial
	}
}

func combinedCacheStatus(statuses map[string]struct{}, hasChatCalls bool) string {
	if !hasChatCalls {
		return "not_applicable"
	}
	if len(statuses) == 0 {
		return "unreported"
	}
	if len(statuses) > 1 {
		return "mixed"
	}
	for status := range statuses {
		return status
	}
	return "unreported"
}

func filterRecordsByCase(records []ModelCallRecord, caseID string) []ModelCallRecord {
	result := make([]ModelCallRecord, 0)
	for _, record := range records {
		if record.CaseID == caseID {
			result = append(result, record)
		}
	}
	return result
}

func modelDurationMS(usage types.EvaluationUsageResult) int64 {
	var total int64
	for _, model := range usage.ByModel {
		total += model.DurationMS
	}
	return total
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1)*p + 0.5)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
