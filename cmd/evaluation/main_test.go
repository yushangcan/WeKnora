package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunEvaluationPollsAndWritesTerminalReport(t *testing.T) {
	const apiKey = "test-secret-key"
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != apiKey {
			t.Fatalf("request did not carry the configured API key")
		}
		if r.Header.Get("X-Tenant-ID") != "10000" {
			t.Fatalf("request did not carry the configured tenant ID")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			var request evaluationRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if request.DatasetID != "default" {
				t.Fatalf("dataset ID = %q, want default", request.DatasetID)
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"task":{"id":"run-1","status":0,"total":1,"finished":0}}}`))
		case http.MethodGet:
			if r.URL.Query().Get("task_id") != "run-1" {
				t.Fatalf("task_id = %q, want run-1", r.URL.Query().Get("task_id"))
			}
			if polls.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"success":true,"data":{"task":{"id":"run-1","status":1,"total":1,"finished":0}}}`))
				return
			}
			_, _ = w.Write([]byte(`{
                    "success": true,
                    "data": {
                        "task": {"id":"run-1","status":2,"total":1,"finished":1},
                        "config": {"config_hash":"sha256:config","runtime":{"metric_version":"retrieval-generation/v2"}},
                        "result": {
                            "run":{"status":"success"},
                            "retrieval":{"precision":0.5},
                            "answer":{"bleu1":0.25},
                            "cost":{"status":"unavailable","amount":null},
                            "timing":{"total_wall_time_ms":125}
                        }
                    }
                }`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	reportPath := filepath.Join(t.TempDir(), "reports", "evaluation.json")
	config := commandConfig{
		BaseURL:      server.URL,
		APIKey:       apiKey,
		TenantID:     "10000",
		DatasetID:    "default",
		PollInterval: time.Millisecond,
		ReportPath:   reportPath,
	}
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runEvaluation(ctx, server.Client(), config, &output); err != nil {
		t.Fatalf("run evaluation: %v", err)
	}
	if polls.Load() != 2 {
		t.Fatalf("poll count = %d, want 2", polls.Load())
	}
	if strings.Contains(output.String(), apiKey) {
		t.Fatal("command output contains the API key")
	}
	if !strings.Contains(output.String(), "retrieval-generation/v2") {
		t.Fatalf("command output does not contain the metric version: %s", output.String())
	}
	for _, fragment := range []string{
		`evaluation retrieval: {"precision":0.5}`,
		`evaluation answer: {"bleu1":0.25}`,
		`evaluation cost: status=unavailable amount=null`,
		`evaluation timing: wall_time_ms=125`,
	} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("command output does not contain %q: %s", fragment, output.String())
		}
	}
	report, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if strings.Contains(string(report), apiKey) {
		t.Fatal("evaluation report contains the API key")
	}
	if !strings.Contains(string(report), `"precision": 0.5`) {
		t.Fatalf("terminal response was not written to the report: %s", report)
	}
}

func TestRunEvaluationWritesFailedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"task":{"id":"run-failed","status":3,"err_msg":"model unavailable"}}}`))
	}))
	defer server.Close()

	reportPath := filepath.Join(t.TempDir(), "failed.json")
	config := commandConfig{
		BaseURL:      server.URL,
		APIKey:       "secret",
		DatasetID:    "default",
		PollInterval: time.Millisecond,
		ReportPath:   reportPath,
	}
	err := runEvaluation(context.Background(), server.Client(), config, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("failure error = %v", err)
	}
	if _, statErr := os.Stat(reportPath); statErr != nil {
		t.Fatalf("failed response report was not written: %v", statErr)
	}
}

func TestRunEvaluationTimeoutWritesLastResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"task":{"id":"run-timeout","status":1,"total":2,"finished":1}}}`))
	}))
	defer server.Close()

	reportPath := filepath.Join(t.TempDir(), "timeout.json")
	config := commandConfig{
		BaseURL:      server.URL,
		APIKey:       "secret",
		DatasetID:    "default",
		PollInterval: time.Second,
		ReportPath:   reportPath,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := runEvaluation(ctx, server.Client(), config, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	if _, statErr := os.Stat(reportPath); statErr != nil {
		t.Fatalf("last response report was not written: %v", statErr)
	}
}

func TestRunEvaluationRequestTimeoutWritesPreviousResponse(t *testing.T) {
	pollStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"success":true,"data":{"task":{"id":"run-request-timeout","status":1,"total":2,"finished":1}}}`))
			return
		}
		close(pollStarted)
		<-r.Context().Done()
	}))
	defer server.Close()

	reportPath := filepath.Join(t.TempDir(), "request-timeout.json")
	config := commandConfig{
		BaseURL:      server.URL,
		APIKey:       "secret",
		DatasetID:    "default",
		PollInterval: time.Millisecond,
		ReportPath:   reportPath,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- runEvaluation(ctx, server.Client(), config, &bytes.Buffer{})
	}()
	select {
	case <-pollStarted:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("poll request did not start")
	}
	var err error
	select {
	case err = <-errCh:
	case <-time.After(time.Second):
		t.Fatal("evaluation did not stop after cancellation")
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	report, readErr := os.ReadFile(reportPath)
	if readErr != nil {
		t.Fatalf("last response report was not written: %v", readErr)
	}
	if !strings.Contains(string(report), `"id": "run-request-timeout"`) {
		t.Fatalf("previous response was not retained: %s", report)
	}
}

func TestLoadCommandConfigRequiresKeyAndValidDurations(t *testing.T) {
	values := map[string]string{
		"WEKNORA_API_KEY":          "secret",
		"EVALUATION_POLL_INTERVAL": "250ms",
		"EVALUATION_TIMEOUT":       "1m",
	}
	config, err := loadCommandConfig(func(name string) string { return values[name] })
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.PollInterval != 250*time.Millisecond || config.Timeout != time.Minute {
		t.Fatalf("unexpected durations: %#v", config)
	}
	delete(values, "WEKNORA_API_KEY")
	if _, err := loadCommandConfig(func(name string) string { return values[name] }); err == nil {
		t.Fatal("missing API key was accepted")
	}
}

func TestRunEvaluationComparisonWritesReportAndPreservesRunOrder(t *testing.T) {
	const apiKey = "compare-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != apiKey || r.Header.Get("X-Tenant-ID") != "10000" {
			t.Fatalf("comparison request lost auth headers")
		}
		if r.URL.Path != "/api/v1/evaluation/comparison" {
			t.Fatalf("comparison path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("baseline_id") != "run-a" || strings.Join(r.URL.Query()["run_ids"], ",") != "run-a,run-b" {
			t.Fatalf("comparison query = %#v", r.URL.Query())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"baseline_id":"run-a","runs":[{"run_id":"run-a"},{"run_id":"run-b"}]}}`))
	}))
	defer server.Close()

	reportPath := filepath.Join(t.TempDir(), "comparison.json")
	config := commandConfig{BaseURL: server.URL, APIKey: apiKey, TenantID: "10000", BaselineRunID: "run-a", ComparisonRunIDs: "run-a,run-b", ComparisonReportPath: reportPath}
	var output bytes.Buffer
	if err := runEvaluationComparison(context.Background(), server.Client(), config, &output); err != nil {
		t.Fatalf("run comparison: %v", err)
	}
	if strings.Contains(output.String(), apiKey) {
		t.Fatal("comparison output contains API key")
	}
	report, err := os.ReadFile(reportPath)
	if err != nil || !strings.Contains(string(report), `"baseline_id": "run-a"`) {
		t.Fatalf("comparison report = %s, err=%v", report, err)
	}
}

func TestSplitRunIDsDeduplicatesAndTrims(t *testing.T) {
	got := splitRunIDs(" run-a,run-b,run-a,, run-c ")
	if strings.Join(got, ",") != "run-a,run-b,run-c" {
		t.Fatalf("splitRunIDs = %#v", got)
	}
}

func TestRunQualityGatePassesWithinTolerance(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "comparison.json")
	report := `{"success":true,"data":{"baseline_id":"run-a","runs":[{"run":{"run_id":"run-a","status":"success"}},{"run":{"run_id":"run-b","status":"success"},"quality_compatibility":{"comparable":true},"quality":{"precision":{"absolute":-0.01},"recall":{"absolute":0.02}}}]}}`
	if err := os.WriteFile(reportPath, []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}
	config := qualityGateConfig{ReportPath: reportPath, Tolerance: 0.01, Metrics: []string{"precision", "recall"}}
	var output bytes.Buffer
	if err := runQualityGate(context.Background(), config, &output); err != nil {
		t.Fatalf("quality gate: %v", err)
	}
	if !strings.Contains(output.String(), "passed") {
		t.Fatalf("quality gate output = %s", output.String())
	}
}

func TestRunQualityGateBlocksRegressionAndIncompatibleRuns(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "comparison.json")
	report := `{"baseline_id":"run-a","runs":[{"run":{"run_id":"run-a","status":"success"}},{"run":{"run_id":"run-b","status":"success"},"quality_compatibility":{"comparable":true},"quality":{"precision":{"absolute":-0.02}}},{"run":{"run_id":"run-c","status":"partial"},"quality_compatibility":{"comparable":false},"quality":{"precision":{"absolute":0.2}}}]}`
	if err := os.WriteFile(reportPath, []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}
	config := qualityGateConfig{ReportPath: reportPath, Tolerance: 0.01, Metrics: []string{"precision"}}
	var output bytes.Buffer
	err := runQualityGate(context.Background(), config, &output)
	if err == nil || !strings.Contains(err.Error(), "quality gate failed") {
		t.Fatalf("quality gate error = %v", err)
	}
	for _, fragment := range []string{"run-b", "run-c", "quality regression"} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("quality gate output missing %q: %s", fragment, output.String())
		}
	}
}

func TestRunQualityGateRejectsMissingBaseline(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "comparison.json")
	report := `{"baseline_id":"run-a","runs":[{"run":{"run_id":"run-b","status":"success"},"quality_compatibility":{"comparable":true},"quality":{"precision":{"absolute":0.02}}},{"run":{"run_id":"run-c","status":"success"},"quality_compatibility":{"comparable":true},"quality":{"precision":{"absolute":0.01}}}]}`
	if err := os.WriteFile(reportPath, []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}
	config := qualityGateConfig{ReportPath: reportPath, Tolerance: 0.01, Metrics: []string{"precision"}}
	var output bytes.Buffer
	err := runQualityGate(context.Background(), config, &output)
	if err == nil || !strings.Contains(err.Error(), "does not contain configured baseline") {
		t.Fatalf("missing baseline error = %v", err)
	}
}

func TestRunQualityGateRejectsUnsuccessfulBaseline(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "comparison.json")
	report := `{"baseline_id":"run-a","runs":[{"run":{"run_id":"run-a","status":"partial"}},{"run":{"run_id":"run-b","status":"success"},"quality_compatibility":{"comparable":true},"quality":{"precision":{"absolute":0.02}}}]}`
	if err := os.WriteFile(reportPath, []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}
	config := qualityGateConfig{ReportPath: reportPath, Tolerance: 0.01, Metrics: []string{"precision"}}
	var output bytes.Buffer
	err := runQualityGate(context.Background(), config, &output)
	if err == nil || !strings.Contains(err.Error(), "is not successful") {
		t.Fatalf("unsuccessful baseline error = %v", err)
	}
}

func TestLoadQualityGateConfigValidatesTolerance(t *testing.T) {
	values := map[string]string{"EVALUATION_QUALITY_TOLERANCE": "-0.1"}
	if _, err := loadQualityGateConfig(func(name string) string { return values[name] }); err == nil {
		t.Fatal("negative quality tolerance was accepted")
	}
}
