ALTER TABLE evaluation_runs
    ADD COLUMN IF NOT EXISTS owner_id VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS lease_until TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS heartbeat_at TIMESTAMP WITH TIME ZONE;

CREATE INDEX IF NOT EXISTS idx_evaluation_runs_lease
    ON evaluation_runs (status, lease_until);
