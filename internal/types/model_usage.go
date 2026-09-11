package types

import "time"

// ModelUsageCacheStatus describes the cache accounting state of one model
// call. A missing report is intentionally different from a reported miss.
type ModelUsageCacheStatus string

const (
	ModelUsageCacheStatusUnsupported ModelUsageCacheStatus = "unsupported"
	ModelUsageCacheStatusUnreported  ModelUsageCacheStatus = "unreported"
	ModelUsageCacheStatusMiss        ModelUsageCacheStatus = "miss"
	ModelUsageCacheStatusHit         ModelUsageCacheStatus = "hit"
)

// ModelUsageCostStatus describes whether a recorded amount is complete and
// backed by a reliable provider or pricing source.
type ModelUsageCostStatus string

const (
	ModelUsageCostStatusAvailable     ModelUsageCostStatus = "available"
	ModelUsageCostStatusPartial       ModelUsageCostStatus = "partial"
	ModelUsageCostStatusUnavailable   ModelUsageCostStatus = "unavailable"
	ModelUsageCostStatusNotApplicable ModelUsageCostStatus = "not_applicable"
)

// ModelUsageSource identifies the product path that initiated a model call.
// It is deliberately a short label rather than a request payload or prompt.
type ModelUsageSource string

const (
	ModelUsageSourceChat       ModelUsageSource = "chat"
	ModelUsageSourceEvaluation ModelUsageSource = "evaluation"
	ModelUsageSourceWiki       ModelUsageSource = "wiki"
	ModelUsageSourceIngestion  ModelUsageSource = "ingestion"
)

// ModelUsageEvent is the durable fact for one real provider round-trip.
// Nullable usage and cost fields preserve the difference between zero and an
// upstream that did not report the value.
type ModelUsageEvent struct {
	ID                uint64                `json:"id" gorm:"primaryKey;autoIncrement"`
	CallID            string                `json:"call_id" gorm:"column:call_id;type:varchar(64);not null;uniqueIndex"`
	TenantID          uint64                `json:"tenant_id" gorm:"column:tenant_id;not null;index:idx_model_usage_tenant_started,priority:1;index:idx_model_usage_tenant_model_started,priority:1;index:idx_model_usage_tenant_type_started,priority:1;index:idx_model_usage_tenant_success_started,priority:1"`
	ModelID           string                `json:"model_id" gorm:"column:model_id;type:varchar(64);not null;index:idx_model_usage_tenant_model_started,priority:2"`
	ModelNameSnapshot string                `json:"model_name" gorm:"column:model_name_snapshot;type:varchar(255);not null"`
	ModelType         ModelType             `json:"model_type" gorm:"column:model_type;type:varchar(32);not null;index:idx_model_usage_tenant_type_started,priority:2"`
	Provider          string                `json:"provider" gorm:"column:provider;type:varchar(64);not null;default:''"`
	Operation         string                `json:"operation" gorm:"column:operation;type:varchar(64);not null"`
	Source            ModelUsageSource      `json:"source" gorm:"column:source;type:varchar(32);not null;default:''"`
	StartedAt         time.Time             `json:"started_at" gorm:"column:started_at;not null;index:idx_model_usage_tenant_started,priority:2,sort:desc;index:idx_model_usage_tenant_model_started,priority:3,sort:desc;index:idx_model_usage_tenant_type_started,priority:3,sort:desc;index:idx_model_usage_tenant_success_started,priority:3,sort:desc"`
	CompletedAt       *time.Time            `json:"completed_at,omitempty" gorm:"column:completed_at"`
	DurationMS        *int64                `json:"duration_ms,omitempty" gorm:"column:duration_ms"`
	Success           bool                  `json:"success" gorm:"column:success;not null;index:idx_model_usage_tenant_success_started,priority:2"`
	ErrorMessage      string                `json:"error_message,omitempty" gorm:"column:error_message;type:text;not null;default:''"`
	ItemCount         int                   `json:"item_count" gorm:"column:item_count;not null;default:1"`
	PromptTokens      *int64                `json:"prompt_tokens,omitempty" gorm:"column:prompt_tokens"`
	CompletionTokens  *int64                `json:"completion_tokens,omitempty" gorm:"column:completion_tokens"`
	TotalTokens       *int64                `json:"total_tokens,omitempty" gorm:"column:total_tokens"`
	CachedTokens      *int64                `json:"cached_tokens,omitempty" gorm:"column:cached_tokens"`
	CacheReadTokens   *int64                `json:"cache_read_tokens,omitempty" gorm:"column:cache_read_tokens"`
	CacheWriteTokens  *int64                `json:"cache_write_tokens,omitempty" gorm:"column:cache_write_tokens"`
	CacheMissTokens   *int64                `json:"cache_miss_tokens,omitempty" gorm:"column:cache_miss_tokens"`
	CacheReported     bool                  `json:"cache_reported" gorm:"column:cache_reported;not null;default:false"`
	CacheStatus       ModelUsageCacheStatus `json:"cache_status" gorm:"column:cache_status;type:varchar(16);not null;default:'unreported'"`
	UsageSource       string                `json:"usage_source" gorm:"column:usage_source;type:varchar(32);not null;default:'unavailable'"`
	CostAmount        *float64              `json:"cost_amount,omitempty" gorm:"column:cost_amount"`
	CostCurrency      string                `json:"cost_currency,omitempty" gorm:"column:cost_currency;type:varchar(8);not null;default:''"`
	PricingVersion    string                `json:"pricing_version,omitempty" gorm:"column:pricing_version;type:varchar(64);not null;default:''"`
	CostSource        string                `json:"cost_source,omitempty" gorm:"column:cost_source;type:varchar(32);not null;default:''"`
	CostStatus        ModelUsageCostStatus  `json:"cost_status" gorm:"column:cost_status;type:varchar(16);not null;default:'unavailable'"`
	SessionID         string                `json:"session_id,omitempty" gorm:"column:session_id;type:varchar(255);not null;default:''"`
	EvaluationRunID   string                `json:"evaluation_run_id,omitempty" gorm:"column:evaluation_run_id;type:varchar(255);not null;index"`
	EvaluationCaseID  string                `json:"evaluation_case_id,omitempty" gorm:"column:evaluation_case_id;type:varchar(128);not null;default:''"`
	TraceID           string                `json:"trace_id,omitempty" gorm:"column:trace_id;type:varchar(255);not null;default:''"`
	ProviderRequestID string                `json:"provider_request_id,omitempty" gorm:"column:provider_request_id;type:varchar(255);not null;default:''"`
	CreatedAt         time.Time             `json:"created_at" gorm:"column:created_at;not null"`
	// ProviderUsage is transient adapter metadata; durable cost fields are
	// resolved by the usage recorder before persistence.
	ProviderUsage *ProviderUsage `json:"-" gorm:"-"`
}

// TableName pins the durable model-call table used by both database drivers.
func (ModelUsageEvent) TableName() string { return "model_usage_events" }

// ModelUsageFilter describes the safe, tenant-scoped filters supported by the
// history endpoint. TenantID is supplied by the authenticated context.
type ModelUsageFilter struct {
	StartedFrom *time.Time
	StartedTo   *time.Time
	ModelID     string
	ModelType   ModelType
	Provider    string
	Operation   string
	Source      ModelUsageSource
	Success     *bool
	Page        int
	PageSize    int
}

// ModelUsageEventPage is a stable time-descending page of call events.
type ModelUsageEventPage struct {
	Items    []ModelUsageEvent `json:"items"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Total    int64             `json:"total"`
}

// ModelUsageSummary is the aggregate for one filter range. CostAmount and
// CacheHitRate stay nil when the required provider data is unavailable.
type ModelUsageSummary struct {
	TotalCalls          int64                `json:"total_calls"`
	SucceededCalls      int64                `json:"succeeded_calls"`
	FailedCalls         int64                `json:"failed_calls"`
	PromptTokens        int64                `json:"prompt_tokens"`
	CompletionTokens    int64                `json:"completion_tokens"`
	TotalTokens         int64                `json:"total_tokens"`
	CachedTokens        int64                `json:"cached_tokens"`
	CacheReadTokens     int64                `json:"cache_read_tokens"`
	CacheWriteTokens    int64                `json:"cache_write_tokens"`
	CacheMissTokens     int64                `json:"cache_miss_tokens"`
	CacheReportedCalls  int64                `json:"cache_reported_calls"`
	CacheHitCalls       int64                `json:"cache_hit_calls"`
	CacheMissCalls      int64                `json:"cache_miss_calls"`
	TokensReportedCalls int64                `json:"tokens_reported_calls"`
	CacheHitRate        *float64             `json:"cache_hit_rate,omitempty"`
	CostAmount          *float64             `json:"cost_amount,omitempty"`
	CostCurrency        string               `json:"cost_currency,omitempty"`
	CostStatus          ModelUsageCostStatus `json:"cost_status"`
	AverageDurationMS   *float64             `json:"average_duration_ms,omitempty"`
	ByModel             []ModelUsageByModel  `json:"by_model"`
}

// ModelUsageByModel contains the same observable counters grouped by the
// model snapshot, so deleted or renamed model configurations remain readable.
type ModelUsageByModel struct {
	ModelID             string               `json:"model_id"`
	ModelName           string               `json:"model_name"`
	ModelType           ModelType            `json:"model_type"`
	Provider            string               `json:"provider"`
	Calls               int64                `json:"calls"`
	SucceededCalls      int64                `json:"succeeded_calls"`
	FailedCalls         int64                `json:"failed_calls"`
	TotalTokens         int64                `json:"total_tokens"`
	PromptTokens        int64                `json:"prompt_tokens"`
	CompletionTokens    int64                `json:"completion_tokens"`
	CacheReadTokens     int64                `json:"cache_read_tokens"`
	CacheWriteTokens    int64                `json:"cache_write_tokens"`
	CacheMissTokens     int64                `json:"cache_miss_tokens"`
	CacheReportedCalls  int64                `json:"cache_reported_calls"`
	CacheHitCalls       int64                `json:"cache_hit_calls"`
	TokensReportedCalls int64                `json:"tokens_reported_calls"`
	CacheHitRate        *float64             `json:"cache_hit_rate,omitempty"`
	CostAmount          *float64             `json:"cost_amount,omitempty"`
	CostCurrency        string               `json:"cost_currency,omitempty"`
	CostStatus          ModelUsageCostStatus `json:"cost_status"`
	AverageDurationMS   *float64             `json:"average_duration_ms,omitempty"`
}
