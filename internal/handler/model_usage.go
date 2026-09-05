package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// ModelUsageHandler serves tenant-scoped model-call history and aggregates.
type ModelUsageHandler struct {
	service interfaces.ModelUsageService
}

// NewModelUsageHandler creates a model usage query handler.
func NewModelUsageHandler(service interfaces.ModelUsageService) *ModelUsageHandler {
	return &ModelUsageHandler{service: service}
}

// ListUsageEvents returns one stable, time-descending page of model calls.
func (h *ModelUsageHandler) ListUsageEvents(c *gin.Context) {
	filter, ok := parseModelUsageFilter(c)
	if !ok {
		return
	}
	result, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		_ = c.Error(apperrors.NewInternalServerError("failed to query model usage events").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// GetUsageSummary returns total and per-model counters for the selected range.
func (h *ModelUsageHandler) GetUsageSummary(c *gin.Context) {
	filter, ok := parseModelUsageFilter(c)
	if !ok {
		return
	}
	result, err := h.service.Summary(c.Request.Context(), filter)
	if err != nil {
		_ = c.Error(apperrors.NewInternalServerError("failed to query model usage summary").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func parseModelUsageFilter(c *gin.Context) (types.ModelUsageFilter, bool) {
	filter := types.ModelUsageFilter{
		ModelID:   secutils.SanitizeForLog(strings.TrimSpace(c.Query("model_id"))),
		ModelType: types.ModelType(secutils.SanitizeForLog(strings.TrimSpace(c.Query("model_type")))),
		Provider:  secutils.SanitizeForLog(strings.TrimSpace(c.Query("provider"))),
		Operation: secutils.SanitizeForLog(strings.TrimSpace(c.Query("operation"))),
		Source:    types.ModelUsageSource(secutils.SanitizeForLog(strings.TrimSpace(c.Query("source")))),
	}
	if err := validateModelUsageFilterValues(filter); err != nil {
		_ = c.Error(apperrors.NewValidationError(err.Error()))
		return types.ModelUsageFilter{}, false
	}
	for _, field := range []struct {
		name string
		dest **time.Time
	}{{"started_from", &filter.StartedFrom}, {"started_to", &filter.StartedTo}} {
		value := strings.TrimSpace(c.Query(field.name))
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			_ = c.Error(apperrors.NewValidationError(field.name + " must use RFC3339 format"))
			return types.ModelUsageFilter{}, false
		}
		*field.dest = &parsed
	}
	if filter.StartedFrom != nil && filter.StartedTo != nil && !filter.StartedFrom.Before(*filter.StartedTo) {
		_ = c.Error(apperrors.NewValidationError("started_from must be earlier than started_to"))
		return types.ModelUsageFilter{}, false
	}
	if raw := strings.TrimSpace(c.Query("success")); raw != "" {
		success, err := strconv.ParseBool(raw)
		if err != nil {
			_ = c.Error(apperrors.NewValidationError("success must be true or false"))
			return types.ModelUsageFilter{}, false
		}
		filter.Success = &success
	}
	page, err := parseModelUsageInt(c.Query("page"), 1)
	if err != nil || page < 1 {
		_ = c.Error(apperrors.NewValidationError("page must be a positive integer"))
		return types.ModelUsageFilter{}, false
	}
	pageSize, err := parseModelUsageInt(c.Query("page_size"), 20)
	if err != nil || pageSize < 1 || pageSize > 100 {
		_ = c.Error(apperrors.NewValidationError("page_size must be between 1 and 100"))
		return types.ModelUsageFilter{}, false
	}
	filter.Page, filter.PageSize = page, pageSize
	return filter, true
}

func validateModelUsageFilterValues(filter types.ModelUsageFilter) error {
	if filter.ModelType != "" {
		switch filter.ModelType {
		case types.ModelTypeKnowledgeQA, types.ModelTypeEmbedding, types.ModelTypeRerank,
			types.ModelTypeVLLM, types.ModelTypeASR:
		default:
			return errors.New("model_type is not supported")
		}
	}
	if filter.Operation != "" {
		switch filter.Operation {
		case "chat", "chat_stream", "embed", "batch_embed", "rerank":
		default:
			return errors.New("operation is not supported")
		}
	}
	if filter.Source != "" {
		switch filter.Source {
		case types.ModelUsageSourceChat, types.ModelUsageSourceEvaluation,
			types.ModelUsageSourceWiki, types.ModelUsageSourceIngestion:
		default:
			return errors.New("source is not supported")
		}
	}
	return nil
}

func parseModelUsageInt(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}
