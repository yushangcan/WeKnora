DROP INDEX IF EXISTS idx_evaluation_runs_lease;
ALTER TABLE evaluation_runs DROP COLUMN heartbeat_at;
ALTER TABLE evaluation_runs DROP COLUMN lease_until;
ALTER TABLE evaluation_runs DROP COLUMN owner_id;
