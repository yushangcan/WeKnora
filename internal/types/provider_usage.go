package types

// ProviderUsage carries provider-specific billable usage alongside a model
// response without changing Chat, Embedder, or Reranker method signatures.
// It is intentionally an observational scope: pricing and persistence are
// handled by later usage-governance layers.
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
