package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEvaluationRunResultJSONContract(t *testing.T) {
	startedAt := time.Date(2026, time.August, 24, 8, 0, 0, 0, time.UTC)
	detail := EvaluationDetail{
		Task: &EvaluationTask{ID: "evaluation-1"},
		Metric: &MetricResult{
			RetrievalMetrics: RetrievalMetrics{Precision: 0.5},
		},
		Result: &EvaluationRunResult{
			SchemaVersion: EvaluationResultSchemaVersion,
			Run: EvaluationRunMetadata{
				RunID:     "evaluation-1",
				TenantID:  7,
				DatasetID: "default",
				StartedAt: startedAt,
				Status:    EvaluationRunStatusRunning,
			},
			Usage: EvaluationUsageResult{
				Status:      EvaluationUsageStatusUnavailable,
				CacheStatus: "unreported",
				ByModel:     []EvaluationModelUsage{},
				ByPhase:     []EvaluationPhaseUsage{},
			},
			Cost: EvaluationCostResult{
				Status:   EvaluationCostStatusUnavailable,
				Source:   "not_reported",
				Amount:   nil,
				Warnings: []EvaluationWarning{},
			},
			Cases:    []EvaluationCaseResult{},
			Warnings: []EvaluationWarning{},
		},
	}

	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	jsonText := string(data)
	for _, expected := range []string{
		`"metric"`,
		`"result"`,
		`"schema_version":"evaluation-run/v1"`,
		`"amount":null`,
	} {
		if !strings.Contains(jsonText, expected) {
			t.Fatalf("JSON %s does not contain %s", jsonText, expected)
		}
	}

	var decoded EvaluationDetail
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if decoded.Metric == nil || decoded.Metric.RetrievalMetrics.Precision != 0.5 {
		t.Fatalf("legacy metric was not preserved: %#v", decoded.Metric)
	}
	if decoded.Result == nil || decoded.Result.Cost.Amount != nil {
		t.Fatalf("nullable cost changed after round trip: %#v", decoded.Result)
	}
}
