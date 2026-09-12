DROP INDEX IF EXISTS idx_evaluation_runs_lease;
ALTER TABLE evaluation_runs
    DROP COLUMN IF EXISTS heartbeat_at,
    DROP COLUMN IF EXISTS lease_until,
    DROP COLUMN IF EXISTS owner_id;
