package types

import "context"

// ProviderUsageSink lets a provider adapter publish response metadata without
// changing the public Embedder/Reranker method signatures.
type ProviderUsageSink interface {
	SetProviderUsage(*ProviderUsage)
}

type providerUsageSinkContextKey struct{}

// WithProviderUsageSink attaches a call-scoped sink used by provider adapters.
func WithProviderUsageSink(ctx context.Context, sink ProviderUsageSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, providerUsageSinkContextKey{}, sink)
}

// RecordProviderUsage publishes provider metadata when the caller installed a
// sink. It is intentionally a no-op for unwrapped direct adapter calls.
func RecordProviderUsage(ctx context.Context, usage *ProviderUsage) {
	if usage == nil {
		return
	}
	if sink, ok := ctx.Value(providerUsageSinkContextKey{}).(ProviderUsageSink); ok && sink != nil {
		sink.SetProviderUsage(usage)
	}
}

// ProviderUsage carries provider-specific billable usage alongside a model
// response without changing Chat, Embedder, or Reranker method signatures.
// It is intentionally an observational scope: pricing and persistence are
// handled by the model usage recorder.
type ProviderUsage struct {
	Tokens        *TokenUsage        `json:"tokens,omitempty"`
	BillableUnits map[string]float64 `json:"billable_units,omitempty"`
	Amount        *float64           `json:"amount,omitempty"`
	// AmountMinor is the provider's exact amount in the currency's minor
	// unit. It takes precedence over Amount when both are present.
	AmountMinor *int64 `json:"amount_minor,omitempty"`
	Currency    string `json:"currency,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	RawSource   string `json:"raw_source,omitempty"`
}

// Clone returns an independent scope so a provider adapter cannot mutate the
// usage event after the wrapper has recorded it.
func (u *ProviderUsage) Clone() *ProviderUsage {
	if u == nil {
		return nil
	}
	clone := *u
	if u.Tokens != nil {
		tokens := *u.Tokens
		clone.Tokens = &tokens
	}
	if u.Amount != nil {
		amount := *u.Amount
		clone.Amount = &amount
	}
	if u.AmountMinor != nil {
		amountMinor := *u.AmountMinor
		clone.AmountMinor = &amountMinor
	}
	if u.BillableUnits != nil {
		clone.BillableUnits = make(map[string]float64, len(u.BillableUnits))
		for key, value := range u.BillableUnits {
			clone.BillableUnits[key] = value
		}
	}
	return &clone
}
