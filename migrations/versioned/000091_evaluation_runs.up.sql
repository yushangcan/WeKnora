CREATE TABLE IF NOT EXISTS evaluation_runs (
    run_id VARCHAR(255) PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    source_knowledge_base_id VARCHAR(36) NOT NULL DEFAULT '',
    temporary_knowledge_base_id VARCHAR(36) NOT NULL DEFAULT '',
    dataset_id VARCHAR(128) NOT NULL,
    dataset_version VARCHAR(64) NOT NULL,
    dataset_fingerprint VARCHAR(80) NOT NULL,
    config_hash VARCHAR(80) NOT NULL,
    embedding_model_id VARCHAR(64) NOT NULL,
    chat_model_id VARCHAR(64) NOT NULL,
    rerank_model_id VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL,
    total INTEGER NOT NULL DEFAULT 0,
    finished INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    config_snapshot JSONB NOT NULL,
    params_snapshot JSONB NOT NULL,
    metric_snapshot JSONB,
    result_snapshot JSONB NOT NULL,
    started_at TIMESTAMP WITH TIME ZONE NOT NULL,
    completed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revision BIGINT NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_evaluation_runs_tenant_created
    ON evaluation_runs (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_evaluation_runs_tenant_status_updated
    ON evaluation_runs (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_evaluation_runs_tenant_config_created
    ON evaluation_runs (tenant_id, config_hash, created_at DESC);

CREATE TABLE IF NOT EXISTS evaluation_run_cases (
    run_id VARCHAR(255) NOT NULL,
    case_id VARCHAR(128) NOT NULL,
    tenant_id INTEGER NOT NULL,
    status VARCHAR(16) NOT NULL,
    started_at TIMESTAMP WITH TIME ZONE NOT NULL,
    completed_at TIMESTAMP WITH TIME ZONE,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    usage_snapshot JSONB NOT NULL,
    warnings_snapshot JSONB NOT NULL,
    result_snapshot JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, case_id),
    FOREIGN KEY (run_id) REFERENCES evaluation_runs(run_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_evaluation_run_cases_tenant_run
    ON evaluation_run_cases (tenant_id, run_id);
