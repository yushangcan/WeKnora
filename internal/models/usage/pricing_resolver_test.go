package usage

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestNewConfiguredPricingResolverReadsCatalog(t *testing.T) {
	t.Setenv(pricingCatalogEnv, `[{"provider":"openai","model":"gpt","unit":"input_tokens","currency":"USD","price_minor_per_unit":2,"pricing_version":"env-v1"}]`)
	resolver, err := NewConfiguredPricingResolver()
	if err != nil {
		t.Fatal(err)
	}
	result := resolver.Resolve("openai", "gpt", &types.ProviderUsage{Currency: "USD", BillableUnits: map[string]float64{"input_tokens": 5}})
	if result.AmountMinor == nil || *result.AmountMinor != 10 || result.PricingVersion != "env-v1" {
		t.Fatalf("configured resolution = %#v", result)
	}
}

func TestNewConfiguredPricingResolverRejectsMalformedCatalog(t *testing.T) {
	t.Setenv(pricingCatalogEnv, "{")
	if _, err := NewConfiguredPricingResolver(); err == nil {
		t.Fatal("malformed catalog was accepted")
	}
}

func TestPricingResolverPrefersProviderMinorAmount(t *testing.T) {
	resolver, err := NewPricingResolver(nil)
	if err != nil {
		t.Fatal(err)
	}
	amount := int64(123)
	result := resolver.Resolve("openai", "gpt", &types.ProviderUsage{
		AmountMinor: &amount,
		Currency:    "usd",
		BillableUnits: map[string]float64{
			"input_tokens": 999,
		},
	})
	if result.Status != types.ModelUsageCostStatusAvailable || result.Source != CostSourceProviderReported || result.AmountMinor == nil || *result.AmountMinor != 123 || result.Currency != "USD" {
		t.Fatalf("provider amount resolution = %#v", result)
	}
}

func TestPricingResolverCalculatesVersionedCatalogInMinorUnits(t *testing.T) {
	resolver, err := NewPricingResolver([]PricingRule{
		{Provider: "openai", Model: "gpt", Unit: "input_tokens", Currency: "USD", PriceMinorPerUnit: 2, PricingVersion: "catalog-2026-09"},
		{Provider: "openai", Model: "gpt", Unit: "output_tokens", Currency: "USD", PriceMinorPerUnit: 4, PricingVersion: "catalog-2026-09"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := resolver.Resolve("openai", "gpt", &types.ProviderUsage{
		Currency: "USD",
		BillableUnits: map[string]float64{
			"input_tokens":  100,
			"output_tokens": 5,
		},
	})
	if result.Status != types.ModelUsageCostStatusAvailable || result.Source != CostSourceCatalogCalculated || result.PricingVersion != "catalog-2026-09" || result.AmountMinor == nil || *result.AmountMinor != 220 {
		t.Fatalf("catalog resolution = %#v", result)
	}
}

func TestPricingResolverDoesNotInventAmountForIncompleteCatalog(t *testing.T) {
	resolver, err := NewPricingResolver([]PricingRule{{Provider: "openai", Model: "gpt", Unit: "input_tokens", Currency: "USD", PriceMinorPerUnit: 2, PricingVersion: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	result := resolver.Resolve("openai", "gpt", &types.ProviderUsage{
		Currency:      "USD",
		BillableUnits: map[string]float64{"input_tokens": 100, "cached_tokens": 20},
	})
	if result.Status != types.ModelUsageCostStatusPartial || result.AmountMinor != nil {
		t.Fatalf("incomplete catalog resolution = %#v", result)
	}
}

func TestPricingResolverRejectsInexactProviderFloatAmount(t *testing.T) {
	resolver, err := NewPricingResolver(nil)
	if err != nil {
		t.Fatal(err)
	}
	amount := 0.123
	result := resolver.Resolve("openai", "gpt", &types.ProviderUsage{Amount: &amount, Currency: "USD"})
	if result.Status != types.ModelUsageCostStatusPartial || result.AmountMinor != nil {
		t.Fatalf("inexact provider amount resolution = %#v", result)
	}
}

func TestPricingResolverRequiresCurrencyAndRejectsInvalidCatalog(t *testing.T) {
	resolver, err := NewPricingResolver([]PricingRule{{Provider: "openai", Model: "gpt", Unit: "input_tokens", Currency: "USD", PriceMinorPerUnit: 2, PricingVersion: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	result := resolver.Resolve("openai", "gpt", &types.ProviderUsage{BillableUnits: map[string]float64{"input_tokens": 1}})
	if result.Status != types.ModelUsageCostStatusUnavailable || result.AmountMinor != nil {
		t.Fatalf("missing currency resolution = %#v", result)
	}
	if _, err := NewPricingResolver([]PricingRule{{Provider: "openai", Model: "gpt", Unit: "input_tokens", Currency: "USD", PricingVersion: "v1"}, {Provider: "openai", Model: "gpt", Unit: "input_tokens", Currency: "USD", PricingVersion: "v2"}}); err == nil {
		t.Fatal("duplicate pricing rule was accepted")
	}
}
