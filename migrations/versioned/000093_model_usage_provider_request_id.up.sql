ALTER TABLE model_usage_events
    ADD COLUMN IF NOT EXISTS provider_request_id VARCHAR(255) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_model_usage_provider_request
    ON model_usage_events (tenant_id, provider, provider_request_id);
