DROP INDEX IF EXISTS idx_evaluation_run_cases_tenant_run;
DROP TABLE IF EXISTS evaluation_run_cases;

DROP INDEX IF EXISTS idx_evaluation_runs_tenant_config_created;
DROP INDEX IF EXISTS idx_evaluation_runs_tenant_status_updated;
DROP INDEX IF EXISTS idx_evaluation_runs_tenant_created;
DROP TABLE IF EXISTS evaluation_runs;
