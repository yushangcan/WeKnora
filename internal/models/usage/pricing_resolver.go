package usage

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// CostSource identifies the evidence used to resolve a cost.
type CostSource string

const (
	CostSourceProviderReported  CostSource = "provider_reported"
	CostSourceCatalogCalculated CostSource = "catalog_calculated"
)

// PricingRule prices one billable unit in integer minor units. Keeping the
// catalog in minor units avoids float accumulation in the resolver.
type PricingRule struct {
	Provider          string
	Model             string
	Unit              string
	Currency          string
	PriceMinorPerUnit int64
	PricingVersion    string
}

type pricingRuleKey struct {
	provider string
	model    string
	unit     string
}

// PricingResolver resolves explicit provider amounts first and falls back to
// a versioned catalog only when the provider supplied billable units.
type PricingResolver struct {
	rules       map[pricingRuleKey]PricingRule
	minorDigits map[string]int
}

// CostResolution is safe to persist into the existing nullable cost fields.
// AmountMinor is the authoritative value; Amount remains nil when no exact
// amount can be established.
type CostResolution struct {
	AmountMinor    *int64
	Currency       string
	PricingVersion string
	Status         types.ModelUsageCostStatus
	Source         CostSource
	Reason         string
}

// AmountMajor converts the exact minor-unit amount to the decimal amount used
// by the existing model_usage_events schema. It returns false when the
// resolution has no exact amount or the currency precision is unknown.
func (r *PricingResolver) AmountMajor(resolution CostResolution) (float64, bool) {
	if r == nil || resolution.AmountMinor == nil || resolution.Currency == "" {
		return 0, false
	}
	digits, ok := r.minorDigits[resolution.Currency]
	if !ok {
		return 0, false
	}
	divisor := 1.0
	for i := 0; i < digits; i++ {
		divisor *= 10
	}
	return float64(*resolution.AmountMinor) / divisor, true
}

// NewPricingResolver validates and freezes a catalog. Duplicate keys or
// missing identity fields are rejected rather than silently selecting a price.
func NewPricingResolver(rules []PricingRule) (*PricingResolver, error) {
	resolver := &PricingResolver{
		rules:       make(map[pricingRuleKey]PricingRule, len(rules)),
		minorDigits: map[string]int{"USD": 2, "EUR": 2, "CNY": 2, "JPY": 0, "KRW": 0},
	}
	for _, rule := range rules {
		rule.Provider = strings.TrimSpace(rule.Provider)
		rule.Model = strings.TrimSpace(rule.Model)
		rule.Unit = strings.TrimSpace(rule.Unit)
		rule.Currency = strings.ToUpper(strings.TrimSpace(rule.Currency))
		rule.PricingVersion = strings.TrimSpace(rule.PricingVersion)
		if rule.Provider == "" || rule.Model == "" || rule.Unit == "" || rule.Currency == "" || rule.PricingVersion == "" {
			return nil, fmt.Errorf("pricing rule identity and version are required")
		}
		if rule.PriceMinorPerUnit < 0 {
			return nil, fmt.Errorf("pricing rule %s/%s/%s has negative price", rule.Provider, rule.Model, rule.Unit)
		}
		key := pricingRuleKey{provider: rule.Provider, model: rule.Model, unit: rule.Unit}
		if _, exists := resolver.rules[key]; exists {
			return nil, fmt.Errorf("duplicate pricing rule %s/%s/%s", rule.Provider, rule.Model, rule.Unit)
		}
		resolver.rules[key] = rule
	}
	return resolver, nil
}

// Resolve applies the evidence policy: explicit Provider amount, then exact
// catalog calculation, otherwise a null amount with an explanatory status.
func (r *PricingResolver) Resolve(provider, model string, usage *types.ProviderUsage) CostResolution {
	base := CostResolution{Status: types.ModelUsageCostStatusUnavailable}
	if r == nil {
		base.Reason = "pricing resolver is unavailable"
		return base
	}
	if usage == nil {
		base.Reason = "provider usage was not reported"
		return base
	}
	currency := strings.ToUpper(strings.TrimSpace(usage.Currency))
	if currency == "" {
		base.Reason = "currency was not reported"
		return base
	}
	base.Currency = currency
	if usage.AmountMinor != nil {
		if *usage.AmountMinor < 0 {
			base.Status = types.ModelUsageCostStatusPartial
			base.Reason = "provider amount is negative"
			return base
		}
		amount := *usage.AmountMinor
		base.AmountMinor = &amount
		base.Status = types.ModelUsageCostStatusAvailable
		base.Source = CostSourceProviderReported
		base.PricingVersion = "provider"
		return base
	}
	if usage.Amount != nil {
		amount, err := decimalMajorToMinor(*usage.Amount, currency, r.minorDigits)
		if err != nil {
			base.Status = types.ModelUsageCostStatusPartial
			base.Reason = "provider amount is not exactly representable in minor units"
			return base
		}
		base.AmountMinor = &amount
		base.Status = types.ModelUsageCostStatusAvailable
		base.Source = CostSourceProviderReported
		base.PricingVersion = "provider"
		return base
	}
	if len(usage.BillableUnits) == 0 {
		base.Reason = "billable units were not reported"
		return base
	}

	units := make([]string, 0, len(usage.BillableUnits))
	for unit := range usage.BillableUnits {
		units = append(units, unit)
	}
	sort.Strings(units)
	var total big.Int
	var version string
	for _, unit := range units {
		value := usage.BillableUnits[unit]
		quantity, err := floatToRat(value)
		if err != nil || quantity.Sign() < 0 {
			base.Status = types.ModelUsageCostStatusPartial
			base.Reason = "billable unit quantity is invalid"
			return base
		}
		rule, ok := r.rules[pricingRuleKey{provider: strings.TrimSpace(provider), model: strings.TrimSpace(model), unit: unit}]
		if !ok || rule.Currency != currency {
			base.Status = types.ModelUsageCostStatusPartial
			base.Reason = "pricing catalog is incomplete for reported units"
			return base
		}
		if version == "" {
			version = rule.PricingVersion
		} else if version != rule.PricingVersion {
			base.Status = types.ModelUsageCostStatusPartial
			base.Reason = "reported units use multiple pricing versions"
			return base
		}
		price := new(big.Rat).SetInt64(rule.PriceMinorPerUnit)
		product := new(big.Rat).Mul(quantity, price)
		if product.Denom().Cmp(big.NewInt(1)) != 0 {
			base.Status = types.ModelUsageCostStatusPartial
			base.Reason = "catalog result is not exactly representable in minor units"
			return base
		}
		total.Add(&total, product.Num())
	}
	amount := total.Int64()
	if !total.IsInt64() {
		base.Status = types.ModelUsageCostStatusPartial
		base.Reason = "catalog result exceeds supported amount range"
		return base
	}
	base.AmountMinor = &amount
	base.PricingVersion = version
	base.Source = CostSourceCatalogCalculated
	base.Status = types.ModelUsageCostStatusAvailable
	return base
}

func floatToRat(value float64) (*big.Rat, error) {
	if value != value || value < 0 {
		return nil, fmt.Errorf("invalid quantity")
	}
	raw := strconv.FormatFloat(value, 'f', -1, 64)
	rat, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, fmt.Errorf("invalid quantity")
	}
	return rat, nil
}

func decimalMajorToMinor(value float64, currency string, digits map[string]int) (int64, error) {
	if value < 0 || value != value {
		return 0, fmt.Errorf("invalid amount")
	}
	places, ok := digits[currency]
	if !ok {
		return 0, fmt.Errorf("unknown currency precision")
	}
	rat, err := floatToRat(value)
	if err != nil {
		return 0, err
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
	rat.Mul(rat, new(big.Rat).SetInt(scale))
	if rat.Denom().Cmp(big.NewInt(1)) != 0 || !rat.Num().IsInt64() {
		return 0, fmt.Errorf("amount has fractional minor units")
	}
	return rat.Num().Int64(), nil
}
