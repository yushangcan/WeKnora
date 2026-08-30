// Package usage contains the cross-model usage recording decorator. It only
// stores call metadata and provider-reported counters; it never handles
// prompts, responses, credentials, retries, or pricing decisions.
package usage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// ModelMetadata is captured when a model client is constructed. Snapshots are
// kept on each event so history remains readable after model edits or deletes.
type ModelMetadata struct {
	ModelID   string
	ModelName string
	ModelType types.ModelType
	Provider  string
	TenantID  uint64
}

// DatabaseRecorder writes completed call facts through the existing database
// repository. Persistence errors are returned to the decorator, which logs
// them without changing the original model result.
type DatabaseRecorder struct {
	repo interfaces.ModelUsageRepository
}

// NewDatabaseRecorder creates a recorder backed by the model usage repository.
func NewDatabaseRecorder(repo interfaces.ModelUsageRepository) interfaces.ModelUsageRecorder {
	return &DatabaseRecorder{repo: repo}
}

func (r *DatabaseRecorder) Record(ctx context.Context, event *types.ModelUsageEvent) error {
	if event == nil {
		return errors.New("model usage event is required")
	}
	if r == nil || r.repo == nil {
		return errors.New("model usage repository is unavailable")
	}
	if event.CallID == "" {
		event.CallID = uuid.NewString()
	}
	if event.CacheStatus == "" {
		event.CacheStatus = types.ModelUsageCacheStatusUnreported
	}
	if event.CostStatus == "" {
		event.CostStatus = types.ModelUsageCostStatusUnavailable
	}
	if event.UsageSource == "" {
		event.UsageSource = "unavailable"
	}
	if event.ItemCount < 1 {
		event.ItemCount = 1
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	return r.repo.Create(ctx, event)
}

func buildEvent(ctx context.Context, metadata ModelMetadata, operation string, startedAt time.Time, itemCount int, success bool, callErr error, tokenUsage *types.TokenUsage) *types.ModelUsageEvent {
	tenantID := metadata.TenantID
	if tenantID == 0 {
		tenantID, _ = types.TenantIDFromContext(ctx)
	}
	if tenantID == 0 || metadata.ModelID == "" || metadata.ModelName == "" {
		return nil
	}
	purpose, _ := types.LLMCallMetadataFromContext(ctx)
	event := &types.ModelUsageEvent{
		CallID:            uuid.NewString(),
		TenantID:          tenantID,
		ModelID:           metadata.ModelID,
		ModelNameSnapshot: metadata.ModelName,
		ModelType:         metadata.ModelType,
		Provider:          metadata.Provider,
		Operation:         operation,
		Source:            sourceForPurpose(purpose),
		StartedAt:         startedAt,
		Success:           success,
		ItemCount:         itemCount,
		CacheStatus:       types.ModelUsageCacheStatusUnreported,
		UsageSource:       "unavailable",
		CostStatus:        types.ModelUsageCostStatusUnavailable,
		EvaluationRunID:   evaluation.CurrentRunID(ctx),
		EvaluationCaseID:  evaluation.CurrentCaseID(ctx),
	}
	if event.Source == "" {
		event.Source = types.ModelUsageSourceChat
	}
	if sessionID, ok := types.SessionIDFromContext(ctx); ok {
		event.SessionID = sessionID
	}
	completedAt := time.Now()
	event.CompletedAt = &completedAt
	duration := completedAt.Sub(startedAt).Milliseconds()
	event.DurationMS = &duration
	if callErr != nil {
		event.ErrorMessage = safeErrorMessage(callErr)
	}
	applyTokenUsage(event, tokenUsage)
	return event
}

func applyTokenUsage(event *types.ModelUsageEvent, usage *types.TokenUsage) {
	if event == nil || usage == nil {
		return
	}
	event.UsageSource = "provider"
	prompt, completion, total := int64(usage.PromptTokens), int64(usage.CompletionTokens), int64(usage.TotalTokens)
	cached, read, write, miss := int64(usage.CachedTokens), int64(usage.CacheReadTokens), int64(usage.CacheWriteTokens), int64(usage.CacheMissTokens)
	event.PromptTokens, event.CompletionTokens, event.TotalTokens = &prompt, &completion, &total
	if usage.CachedTokens != 0 || usage.CacheReadTokens != 0 || usage.CacheWriteTokens != 0 || usage.CacheMissTokens != 0 || usage.CacheReported {
		event.CachedTokens, event.CacheReadTokens, event.CacheWriteTokens, event.CacheMissTokens = &cached, &read, &write, &miss
	}
	event.CacheReported = usage.CacheReported
	if usage.CacheStatus != "" {
		event.CacheStatus = types.ModelUsageCacheStatus(usage.CacheStatus)
	} else if usage.CacheReported {
		if usage.CacheReadTokens > 0 {
			event.CacheStatus = types.ModelUsageCacheStatusHit
		} else {
			event.CacheStatus = types.ModelUsageCacheStatusMiss
		}
	}
}

func sourceForPurpose(purpose string) types.ModelUsageSource {
	switch strings.ToLower(strings.TrimSpace(purpose)) {
	case "evaluation", "eval":
		return types.ModelUsageSourceEvaluation
	case "wiki", "wiki_page_modify", "wiki_ingest":
		return types.ModelUsageSourceWiki
	case "ingestion", "document_ingestion", "document_parse":
		return types.ModelUsageSourceIngestion
	default:
		return types.ModelUsageSourceChat
	}
}

func safeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	// Keep the type and a bounded, single-line message for diagnostics while
	// avoiding full provider payloads and potentially embedded credentials.
	message := strings.Join(strings.Fields(err.Error()), " ")
	if len(message) > 256 {
		message = message[:256]
	}
	return fmt.Sprintf("%T: %s", err, message)
}

func recordEvent(ctx context.Context, recorder interfaces.ModelUsageRecorder, event *types.ModelUsageEvent) {
	if recorder == nil || event == nil {
		return
	}
	_ = recorder.Record(ctx, event)
}
