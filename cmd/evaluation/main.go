package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	evaluationPending = iota
	evaluationRunning
	evaluationSuccess
	evaluationFailed
)

type commandConfig struct {
	BaseURL              string
	APIKey               string
	TenantID             string
	DatasetID            string
	KnowledgeBaseID      string
	ChatModelID          string
	RerankModelID        string
	PollInterval         time.Duration
	Timeout              time.Duration
	ReportPath           string
	BaselineRunID        string
	ComparisonRunIDs     string
	ComparisonReportPath string
}

type evaluationRequest struct {
	DatasetID       string `json:"dataset_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	ChatModelID     string `json:"chat_id"`
	RerankModelID   string `json:"rerank_id"`
}

type evaluationResponse struct {
	Success bool             `json:"success"`
	Data    evaluationDetail `json:"data"`
}

type evaluationComparisonResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

type evaluationDetail struct {
	Task struct {
		ID       string `json:"id"`
		Status   int    `json:"status"`
		ErrMsg   string `json:"err_msg"`
		Total    int    `json:"total"`
		Finished int    `json:"finished"`
	} `json:"task"`
	Config *struct {
		ConfigHash string `json:"config_hash"`
		Runtime    struct {
			MetricVersion string `json:"metric_version"`
		} `json:"runtime"`
	} `json:"config"`
	Result *struct {
		Run struct {
			Status string `json:"status"`
		} `json:"run"`
		Retrieval json.RawMessage `json:"retrieval"`
		Answer    json.RawMessage `json:"answer"`
		Cost      struct {
			Status string   `json:"status"`
			Amount *float64 `json:"amount"`
		} `json:"cost"`
		Timing struct {
			TotalWallTimeMS int64 `json:"total_wall_time_ms"`
		} `json:"timing"`
	} `json:"result"`
}

func main() {
	config, err := loadCommandConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	var runErr error
	if len(os.Args) > 1 && strings.EqualFold(strings.TrimSpace(os.Args[1]), "compare") {
		runErr = runEvaluationComparison(ctx, http.DefaultClient, config, os.Stdout)
	} else {
		runErr = runEvaluation(ctx, http.DefaultClient, config, os.Stdout)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		os.Exit(1)
	}
}

func loadCommandConfig(getenv func(string) string) (commandConfig, error) {
	config := commandConfig{
		BaseURL:              envOrDefault(getenv, "WEKNORA_BASE_URL", "http://localhost:8080"),
		APIKey:               strings.TrimSpace(getenv("WEKNORA_API_KEY")),
		TenantID:             strings.TrimSpace(getenv("WEKNORA_TENANT_ID")),
		DatasetID:            envOrDefault(getenv, "EVALUATION_DATASET_ID", "default"),
		KnowledgeBaseID:      strings.TrimSpace(getenv("EVALUATION_KNOWLEDGE_BASE_ID")),
		ChatModelID:          strings.TrimSpace(getenv("EVALUATION_CHAT_MODEL_ID")),
		RerankModelID:        strings.TrimSpace(getenv("EVALUATION_RERANK_MODEL_ID")),
		ReportPath:           envOrDefault(getenv, "EVALUATION_REPORT_PATH", "tmp/evaluation-report.json"),
		BaselineRunID:        strings.TrimSpace(getenv("EVALUATION_BASELINE_RUN_ID")),
		ComparisonRunIDs:     strings.TrimSpace(getenv("EVALUATION_COMPARISON_RUN_IDS")),
		ComparisonReportPath: envOrDefault(getenv, "EVALUATION_COMPARISON_REPORT_PATH", "tmp/evaluation-comparison.json"),
	}
	if config.APIKey == "" {
		return commandConfig{}, errors.New("WEKNORA_API_KEY is required")
	}
	var err error
	config.PollInterval, err = envDuration(getenv, "EVALUATION_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		return commandConfig{}, err
	}
	config.Timeout, err = envDuration(getenv, "EVALUATION_TIMEOUT", 30*time.Minute)
	if err != nil {
		return commandConfig{}, err
	}
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	parsedURL, err := url.ParseRequestURI(config.BaseURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return commandConfig{}, fmt.Errorf("WEKNORA_BASE_URL must be an absolute HTTP(S) URL")
	}
	return config, nil
}

func runEvaluationComparison(ctx context.Context, httpClient *http.Client, config commandConfig, output io.Writer) error {
	baselineID := strings.TrimSpace(config.BaselineRunID)
	if baselineID == "" {
		return errors.New("EVALUATION_BASELINE_RUN_ID is required for compare")
	}
	runIDs := splitRunIDs(config.ComparisonRunIDs)
	if len(runIDs) < 2 {
		return errors.New("EVALUATION_COMPARISON_RUN_IDS must contain at least two unique run IDs")
	}
	foundBaseline := false
	for _, runID := range runIDs {
		if runID == baselineID {
			foundBaseline = true
			break
		}
	}
	if !foundBaseline {
		return errors.New("EVALUATION_BASELINE_RUN_ID must be included in EVALUATION_COMPARISON_RUN_IDS")
	}
	query := url.Values{}
	query.Set("baseline_id", baselineID)
	for _, runID := range runIDs {
		query.Add("run_ids", runID)
	}
	endpoint := config.BaseURL + "/api/v1/evaluation/comparison?" + query.Encode()
	response, rawResponse, err := callEvaluationComparisonAPI(ctx, httpClient, config, endpoint)
	if err != nil {
		return fmt.Errorf("compare evaluation runs: %w", err)
	}
	if err := writeEvaluationReport(config.ComparisonReportPath, rawResponse); err != nil {
		return err
	}
	if len(response.Data) == 0 || string(response.Data) == "null" {
		return errors.New("comparison response does not contain data")
	}
	fingerprint := compactJSON(response.Data, "unavailable")
	fmt.Fprintf(output, "evaluation comparison: baseline=%s runs=%d\n", baselineID, len(runIDs))
	fmt.Fprintf(output, "evaluation comparison data: %s\n", fingerprint)
	fmt.Fprintf(output, "evaluation comparison report: %s\n", config.ComparisonReportPath)
	return nil
}

func splitRunIDs(raw string) []string {
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}
	return ids
}

func callEvaluationComparisonAPI(ctx context.Context, httpClient *http.Client, config commandConfig, endpoint string) (evaluationComparisonResponse, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return evaluationComparisonResponse{}, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-API-Key", config.APIKey)
	if config.TenantID != "" {
		request.Header.Set("X-Tenant-ID", config.TenantID)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return evaluationComparisonResponse{}, nil, err
	}
	defer response.Body.Close()
	rawResponse, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return evaluationComparisonResponse{}, nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return evaluationComparisonResponse{}, rawResponse, fmt.Errorf("API returned HTTP %d", response.StatusCode)
	}
	var decoded evaluationComparisonResponse
	if err := json.Unmarshal(rawResponse, &decoded); err != nil {
		return evaluationComparisonResponse{}, rawResponse, fmt.Errorf("decode comparison response: %w", err)
	}
	if !decoded.Success {
		return evaluationComparisonResponse{}, rawResponse, errors.New("API reported an unsuccessful comparison response")
	}
	return decoded, rawResponse, nil
}

func envOrDefault(getenv func(string) string, name, fallback string) string {
	if value := strings.TrimSpace(getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envDuration(getenv func(string) string, name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return duration, nil
}

func runEvaluation(
	ctx context.Context,
	httpClient *http.Client,
	config commandConfig,
	output io.Writer,
) error {
	request := evaluationRequest{
		DatasetID:       config.DatasetID,
		KnowledgeBaseID: config.KnowledgeBaseID,
		ChatModelID:     config.ChatModelID,
		RerankModelID:   config.RerankModelID,
	}
	response, rawResponse, err := callEvaluationAPI(
		ctx,
		httpClient,
		config,
		http.MethodPost,
		config.BaseURL+"/api/v1/evaluation",
		request,
	)
	if err != nil {
		return fmt.Errorf("start evaluation: %w", err)
	}
	if response.Data.Task.ID == "" {
		return errors.New("start evaluation: response does not contain a task ID")
	}
	taskID := response.Data.Task.ID
	fmt.Fprintf(output, "evaluation task started: %s\n", taskID)
	lastProgress := ""

	for {
		progress := fmt.Sprintf("%d/%d", response.Data.Task.Finished, response.Data.Task.Total)
		if progress != lastProgress {
			fmt.Fprintf(output, "evaluation progress: %s\n", progress)
			lastProgress = progress
		}
		switch response.Data.Task.Status {
		case evaluationSuccess:
			if err := writeEvaluationReport(config.ReportPath, rawResponse); err != nil {
				return err
			}
			printEvaluationSummary(output, response.Data, config.ReportPath)
			return nil
		case evaluationFailed:
			if err := writeEvaluationReport(config.ReportPath, rawResponse); err != nil {
				return err
			}
			return fmt.Errorf(
				"evaluation task %s failed: %s (report: %s)",
				taskID,
				emptyFallback(response.Data.Task.ErrMsg, "no error message reported"),
				config.ReportPath,
			)
		case evaluationPending, evaluationRunning:
		default:
			return fmt.Errorf("evaluation task %s returned unknown status %d", taskID, response.Data.Task.Status)
		}

		select {
		case <-ctx.Done():
			return evaluationTimeoutError(taskID, config.ReportPath, rawResponse)
		case <-time.After(config.PollInterval):
		}

		nextResponse, nextRawResponse, pollErr := callEvaluationAPI(
			ctx,
			httpClient,
			config,
			http.MethodGet,
			config.BaseURL+"/api/v1/evaluation?task_id="+url.QueryEscape(taskID),
			nil,
		)
		if pollErr != nil {
			if ctx.Err() != nil {
				return evaluationTimeoutError(taskID, config.ReportPath, rawResponse)
			}
			return fmt.Errorf("poll evaluation task %s: %w", taskID, pollErr)
		}
		response, rawResponse = nextResponse, nextRawResponse
	}
}

func evaluationTimeoutError(taskID, reportPath string, rawResponse []byte) error {
	if reportErr := writeEvaluationReport(reportPath, rawResponse); reportErr != nil {
		return fmt.Errorf("evaluation task %s timed out; save last response: %w", taskID, reportErr)
	}
	return fmt.Errorf("evaluation task %s timed out (last response: %s)", taskID, reportPath)
}

func callEvaluationAPI(
	ctx context.Context,
	httpClient *http.Client,
	config commandConfig,
	method, endpoint string,
	body interface{},
) (evaluationResponse, []byte, error) {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return evaluationResponse{}, nil, err
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, requestBody)
	if err != nil {
		return evaluationResponse{}, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-API-Key", config.APIKey)
	if config.TenantID != "" {
		request.Header.Set("X-Tenant-ID", config.TenantID)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return evaluationResponse{}, nil, err
	}
	defer response.Body.Close()
	rawResponse, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return evaluationResponse{}, nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return evaluationResponse{}, rawResponse, fmt.Errorf("API returned HTTP %d", response.StatusCode)
	}
	var decoded evaluationResponse
	if err := json.Unmarshal(rawResponse, &decoded); err != nil {
		return evaluationResponse{}, rawResponse, fmt.Errorf("decode API response: %w", err)
	}
	if !decoded.Success {
		return evaluationResponse{}, rawResponse, errors.New("API reported an unsuccessful response")
	}
	return decoded, rawResponse, nil
}

func writeEvaluationReport(path string, rawResponse []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("EVALUATION_REPORT_PATH cannot be empty")
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, rawResponse, "", "  "); err != nil {
		return fmt.Errorf("format evaluation report: %w", err)
	}
	formatted.WriteByte('\n')
	directory := filepath.Dir(path)
	if directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create evaluation report directory: %w", err)
		}
	}
	if err := os.WriteFile(path, formatted.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write evaluation report: %w", err)
	}
	return nil
}

func printEvaluationSummary(output io.Writer, detail evaluationDetail, reportPath string) {
	configHash := "unavailable"
	metricVersion := "unavailable"
	if detail.Config != nil {
		configHash = emptyFallback(detail.Config.ConfigHash, configHash)
		metricVersion = emptyFallback(detail.Config.Runtime.MetricVersion, metricVersion)
	}
	status := "success"
	retrieval := "unavailable"
	answer := "unavailable"
	costStatus := "unavailable"
	costAmount := "null"
	wallTime := int64(0)
	if detail.Result != nil {
		status = emptyFallback(detail.Result.Run.Status, status)
		retrieval = compactJSON(detail.Result.Retrieval, retrieval)
		answer = compactJSON(detail.Result.Answer, answer)
		costStatus = emptyFallback(detail.Result.Cost.Status, costStatus)
		if detail.Result.Cost.Amount != nil {
			costAmount = fmt.Sprintf("%g", *detail.Result.Cost.Amount)
		}
		wallTime = detail.Result.Timing.TotalWallTimeMS
	}
	fmt.Fprintf(output, "evaluation completed: status=%s config_hash=%s metric_version=%s\n",
		status, configHash, metricVersion)
	fmt.Fprintf(output, "evaluation retrieval: %s\n", retrieval)
	fmt.Fprintf(output, "evaluation answer: %s\n", answer)
	fmt.Fprintf(output, "evaluation cost: status=%s amount=%s\n", costStatus, costAmount)
	fmt.Fprintf(output, "evaluation timing: wall_time_ms=%d\n", wallTime)
	fmt.Fprintf(output, "evaluation report: %s\n", reportPath)
}

func compactJSON(value json.RawMessage, fallback string) string {
	if len(value) == 0 || string(value) == "null" {
		return fallback
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, value); err != nil {
		return fallback
	}
	return compact.String()
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
