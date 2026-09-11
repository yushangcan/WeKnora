package usage

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type pricingRepositoryStub struct {
	event *types.ModelUsageEvent
}

func (r *pricingRepositoryStub) Create(_ context.Context, event *types.ModelUsageEvent) error {
	r.event = event
	return nil
}

func (*pricingRepositoryStub) List(context.Context, uint64, types.ModelUsageFilter) (*types.ModelUsageEventPage, error) {
	return nil, nil
}

func (*pricingRepositoryStub) Summary(context.Context, uint64, types.ModelUsageFilter) (*types.ModelUsageSummary, error) {
	return nil, nil
}

func TestDatabaseRecorderPersistsResolvedProviderCost(t *testing.T) {
	repo := &pricingRepositoryStub{}
	amount := int64(123)
	recorder := NewDatabaseRecorder(repo)
	err := recorder.Record(context.Background(), &types.ModelUsageEvent{
		TenantID: 1, ModelID: "model-id", ModelNameSnapshot: "model-name", Provider: "aliyun",
		ProviderUsage: &types.ProviderUsage{AmountMinor: &amount, Currency: "cny"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.event == nil || repo.event.CostAmount == nil {
		t.Fatalf("resolved event = %#v", repo.event)
	}
	if got := *repo.event.CostAmount; got != 1.23 {
		t.Fatalf("cost amount = %v, want 1.23", got)
	}
	if repo.event.CostCurrency != "CNY" || repo.event.PricingVersion != "provider" || repo.event.CostStatus != types.ModelUsageCostStatusAvailable {
		t.Fatalf("cost metadata = currency %q version %q status %q", repo.event.CostCurrency, repo.event.PricingVersion, repo.event.CostStatus)
	}
	if repo.event.UsageSource != string(CostSourceProviderReported) {
		t.Fatalf("usage source = %q", repo.event.UsageSource)
	}
}

func TestDatabaseRecorderPersistsCatalogCost(t *testing.T) {
	repo := &pricingRepositoryStub{}
	resolver, err := NewPricingResolver([]PricingRule{{
		Provider: "openai", Model: "embed", Unit: "input_tokens", Currency: "USD",
		PriceMinorPerUnit: 2, PricingVersion: "catalog-test",
	}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := NewDatabaseRecorder(repo, resolver)
	err = recorder.Record(context.Background(), &types.ModelUsageEvent{
		TenantID: 1, ModelID: "model-id", ModelNameSnapshot: "embed", Provider: "openai",
		ProviderUsage: &types.ProviderUsage{Currency: "USD", BillableUnits: map[string]float64{"input_tokens": 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.event == nil || repo.event.CostAmount == nil || *repo.event.CostAmount != 2 {
		t.Fatalf("catalog event = %#v", repo.event)
	}
	if repo.event.PricingVersion != "catalog-test" || repo.event.UsageSource != string(CostSourceCatalogCalculated) {
		t.Fatalf("catalog metadata = version %q source %q", repo.event.PricingVersion, repo.event.UsageSource)
	}
}
