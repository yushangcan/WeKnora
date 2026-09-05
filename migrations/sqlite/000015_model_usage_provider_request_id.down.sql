DROP INDEX IF EXISTS idx_model_usage_provider_request;

ALTER TABLE model_usage_events
    DROP COLUMN provider_request_id;
