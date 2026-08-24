package evaluation

import (
	"context"
	"fmt"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

// ModelCallRecord contains only non-sensitive metadata needed by stage-one
// aggregation. Prompt, answer, document text, credentials and raw provider
// payloads are intentionally excluded.
type ModelCallRecord struct {
	RunID       string
	CaseID      string
	Phase       types.EvaluationPhase
	ModelType   types.EvaluationModelType
	ModelID     string
	ModelName   string
	Operation   types.EvaluationModelOperation
	Success     bool
	DurationMS  int64
	CallCount   int
	ItemCount   int
	UsageSource types.EvaluationUsageSource
	Usage       *types.TokenUsage
	ErrorType   string
}

type ModelCallCollector struct {
	mu      sync.RWMutex
	records []ModelCallRecord
}

func NewModelCallCollector() *ModelCallCollector {
	return &ModelCallCollector{}
}

func (c *ModelCallCollector) Record(record ModelCallRecord) {
	if c == nil {
		return
	}
	if record.CallCount <= 0 {
		record.CallCount = 1
	}
	if record.ItemCount < 0 {
		record.ItemCount = 0
	}
	record.Usage = cloneTokenUsage(record.Usage)
	c.mu.Lock()
	c.records = append(c.records, record)
	c.mu.Unlock()
}

func (c *ModelCallCollector) Snapshot() []ModelCallRecord {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]ModelCallRecord, len(c.records))
	for i, record := range c.records {
		result[i] = record
		result[i].Usage = cloneTokenUsage(record.Usage)
	}
	return result
}

// RecordModelCall records a call only when ctx belongs to an evaluation run.
// Outside evaluation this function is a no-op.
func RecordModelCall(ctx context.Context, record ModelCallRecord, callErr error) {
	scope, ok := scopeFromContext(ctx)
	if !ok {
		return
	}
	record.RunID = scope.observer.RunID()
	record.CaseID = scope.caseID
	record.Phase = scope.phase
	record.Success = callErr == nil
	if callErr != nil {
		// Store only the concrete error category. Raw provider messages can
		// contain endpoints, request fragments or credentials.
		record.ErrorType = fmt.Sprintf("%T", callErr)
	}
	scope.observer.collector.Record(record)
}

func cloneTokenUsage(usage *types.TokenUsage) *types.TokenUsage {
	if usage == nil {
		return nil
	}
	copy := *usage
	return &copy
}
