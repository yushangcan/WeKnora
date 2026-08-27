package handler

import (
	stderrors "errors"
	"net/http"
	"strings"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ListEvaluationRuns returns one filtered page of persisted runs.
func (e *EvaluationHandler) ListEvaluationRuns(c *gin.Context) {
	page, pageSize, ok := parseListPagination(c)
	if !ok {
		return
	}
	startedFrom, ok := parseEvaluationHistoryTime(c, "started_from")
	if !ok {
		return
	}
	startedTo, ok := parseEvaluationHistoryTime(c, "started_to")
	if !ok {
		return
	}
	result, err := e.evaluationService.ListEvaluationRuns(c.Request.Context(), types.EvaluationRunListFilter{
		Status:           types.EvaluationRunStatus(secutils.SanitizeForLog(c.Query("status"))),
		DatasetID:        secutils.SanitizeForLog(c.Query("dataset_id")),
		ConfigHash:       secutils.SanitizeForLog(c.Query("config_hash")),
		EmbeddingModelID: secutils.SanitizeForLog(c.Query("embedding_model_id")),
		ChatModelID:      secutils.SanitizeForLog(c.Query("chat_model_id")),
		RerankModelID:    secutils.SanitizeForLog(c.Query("rerank_model_id")),
		StartedFrom:      startedFrom,
		StartedTo:        startedTo,
		Page:             page,
		PageSize:         pageSize,
	})
	if err != nil {
		handleEvaluationHistoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// GetEvaluationRun returns one lightweight persisted run overview.
func (e *EvaluationHandler) GetEvaluationRun(c *gin.Context) {
	runID := secutils.SanitizeForLog(c.Param("run_id"))
	result, err := e.evaluationService.GetEvaluationRun(c.Request.Context(), runID)
	if err != nil {
		handleEvaluationHistoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// ListEvaluationRunCases returns one page of case audit evidence.
func (e *EvaluationHandler) ListEvaluationRunCases(c *gin.Context) {
	page, pageSize, ok := parseListPagination(c)
	if !ok {
		return
	}
	result, err := e.evaluationService.ListEvaluationRunCases(
		c.Request.Context(),
		secutils.SanitizeForLog(c.Param("run_id")),
		types.EvaluationRunStatus(secutils.SanitizeForLog(c.Query("status"))),
		page,
		pageSize,
	)
	if err != nil {
		handleEvaluationHistoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// CompareEvaluationRuns returns baseline-relative deltas for persisted runs.
func (e *EvaluationHandler) CompareEvaluationRuns(c *gin.Context) {
	values := make([]string, 0)
	for _, raw := range c.QueryArray("run_ids") {
		for _, value := range strings.Split(raw, ",") {
			if value = secutils.SanitizeForLog(value); value != "" {
				values = append(values, value)
			}
		}
	}
	result, err := e.evaluationService.CompareEvaluationRuns(
		c.Request.Context(),
		secutils.SanitizeForLog(c.Query("baseline_id")),
		values,
	)
	if err != nil {
		handleEvaluationHistoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func parseEvaluationHistoryTime(c *gin.Context, name string) (*time.Time, bool) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		_ = c.Error(apperrors.NewValidationError(name + " must use RFC3339 format"))
		return nil, false
	}
	return &parsed, true
}

func handleEvaluationHistoryError(c *gin.Context, err error) {
	var appError *apperrors.AppError
	switch {
	case stderrors.As(err, &appError):
		_ = c.Error(appError)
	case stderrors.Is(err, gorm.ErrRecordNotFound):
		_ = c.Error(apperrors.NewNotFoundError("evaluation run not found"))
	default:
		_ = c.Error(apperrors.NewInternalServerError("failed to query evaluation history").WithDetails(err.Error()))
	}
}
