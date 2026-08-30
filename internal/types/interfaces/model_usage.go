package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// ModelUsageRepository persists and aggregates one event per real model
// provider round-trip.
type ModelUsageRepository interface {
	Create(ctx context.Context, event *types.ModelUsageEvent) error
	List(ctx context.Context, tenantID uint64, filter types.ModelUsageFilter) (*types.ModelUsageEventPage, error)
	Summary(ctx context.Context, tenantID uint64, filter types.ModelUsageFilter) (*types.ModelUsageSummary, error)
}

// ModelUsageRecorder accepts completed model-call events. Implementations must
// not change the result of the original provider call when persistence fails.
type ModelUsageRecorder interface {
	Record(ctx context.Context, event *types.ModelUsageEvent) error
}
