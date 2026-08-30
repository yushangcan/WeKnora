package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type modelUsageService struct {
	repo interfaces.ModelUsageRepository
}

// NewModelUsageService creates the tenant-scoped model usage query service.
func NewModelUsageService(repo interfaces.ModelUsageRepository) interfaces.ModelUsageService {
	return &modelUsageService{repo: repo}
}

func (s *modelUsageService) List(ctx context.Context, filter types.ModelUsageFilter) (*types.ModelUsageEventPage, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, errors.New("tenant ID is required for model usage query")
	}
	return s.repo.List(ctx, tenantID, filter)
}

func (s *modelUsageService) Summary(ctx context.Context, filter types.ModelUsageFilter) (*types.ModelUsageSummary, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, errors.New("tenant ID is required for model usage query")
	}
	return s.repo.Summary(ctx, tenantID, filter)
}

var _ interfaces.ModelUsageService = (*modelUsageService)(nil)
