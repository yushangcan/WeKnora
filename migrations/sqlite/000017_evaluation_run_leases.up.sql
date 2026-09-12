ALTER TABLE evaluation_runs ADD COLUMN owner_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE evaluation_runs ADD COLUMN lease_until DATETIME;
ALTER TABLE evaluation_runs ADD COLUMN heartbeat_at DATETIME;
CREATE INDEX IF NOT EXISTS idx_evaluation_runs_lease ON evaluation_runs(status, lease_until);
