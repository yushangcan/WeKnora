CREATE TABLE IF NOT EXISTS model_usage_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    call_id VARCHAR(64) NOT NULL UNIQUE,
    tenant_id INTEGER NOT NULL,
    model_id VARCHAR(64) NOT NULL,
    model_name_snapshot VARCHAR(255) NOT NULL,
    model_type VARCHAR(32) NOT NULL,
    provider VARCHAR(64) NOT NULL DEFAULT '',
    operation VARCHAR(64) NOT NULL,
    source VARCHAR(32) NOT NULL DEFAULT '',
    started_at DATETIME NOT NULL,
    completed_at DATETIME,
    duration_ms INTEGER,
    success INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    item_count INTEGER NOT NULL DEFAULT 1,
    prompt_tokens INTEGER,
    completion_tokens INTEGER,
    total_tokens INTEGER,
    cached_tokens INTEGER,
    cache_read_tokens INTEGER,
    cache_write_tokens INTEGER,
    cache_miss_tokens INTEGER,
    cache_reported INTEGER NOT NULL DEFAULT 0,
    cache_status VARCHAR(16) NOT NULL DEFAULT 'unreported',
    usage_source VARCHAR(32) NOT NULL DEFAULT 'unavailable',
    cost_amount REAL,
    cost_currency VARCHAR(8) NOT NULL DEFAULT '',
    pricing_version VARCHAR(64) NOT NULL DEFAULT '',
    cost_status VARCHAR(16) NOT NULL DEFAULT 'unavailable',
    session_id VARCHAR(255) NOT NULL DEFAULT '',
    evaluation_run_id VARCHAR(255) NOT NULL DEFAULT '',
    evaluation_case_id VARCHAR(128) NOT NULL DEFAULT '',
    trace_id VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_model_usage_tenant_started
    ON model_usage_events (tenant_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_usage_tenant_model_started
    ON model_usage_events (tenant_id, model_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_usage_tenant_type_started
    ON model_usage_events (tenant_id, model_type, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_usage_tenant_success_started
    ON model_usage_events (tenant_id, success, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_usage_evaluation_run
    ON model_usage_events (evaluation_run_id);
