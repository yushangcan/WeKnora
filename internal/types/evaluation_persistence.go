package types

import "time"

const EvaluationRunLeaseDuration = 2 * time.Minute

// EvaluationRunRecord stores the durable state and immutable snapshots for one run.
type EvaluationRunRecord struct {
	RunID                    string              `gorm:"column:run_id;type:varchar(255);primaryKey"`
	TenantID                 uint64              `gorm:"column:tenant_id;not null;index:idx_evaluation_runs_tenant_created,priority:1;index:idx_evaluation_runs_tenant_status_updated,priority:1;index:idx_evaluation_runs_tenant_config_created,priority:1"`
	SourceKnowledgeBaseID    string              `gorm:"column:source_knowledge_base_id;type:varchar(36);not null;default:''"`
	TemporaryKnowledgeBaseID string              `gorm:"column:temporary_knowledge_base_id;type:varchar(36);not null;default:''"`
	DatasetID                string              `gorm:"column:dataset_id;type:varchar(128);not null"`
	DatasetVersion           string              `gorm:"column:dataset_version;type:varchar(64);not null"`
	DatasetFingerprint       string              `gorm:"column:dataset_fingerprint;type:varchar(80);not null"`
	ConfigHash               string              `gorm:"column:config_hash;type:varchar(80);not null;index:idx_evaluation_runs_tenant_config_created,priority:2"`
	EmbeddingModelID         string              `gorm:"column:embedding_model_id;type:varchar(64);not null"`
	ChatModelID              string              `gorm:"column:chat_model_id;type:varchar(64);not null"`
	RerankModelID            string              `gorm:"column:rerank_model_id;type:varchar(64);not null;default:''"`
	Status                   EvaluationRunStatus `gorm:"column:status;type:varchar(16);not null;index:idx_evaluation_runs_tenant_status_updated,priority:2"`
	Total                    int                 `gorm:"column:total;not null;default:0"`
	Finished                 int                 `gorm:"column:finished;not null;default:0"`
	ErrorMessage             string              `gorm:"column:error_message;type:text;not null;default:''"`
	ConfigSnapshot           JSON                `gorm:"column:config_snapshot;type:json;not null"`
	ParamsSnapshot           JSON                `gorm:"column:params_snapshot;type:json;not null"`
	MetricSnapshot           JSON                `gorm:"column:metric_snapshot;type:json"`
	ResultSnapshot           JSON                `gorm:"column:result_snapshot;type:json;not null"`
	StartedAt                time.Time           `gorm:"column:started_at;not null"`
	CompletedAt              *time.Time          `gorm:"column:completed_at"`
	CreatedAt                time.Time           `gorm:"column:created_at;not null;index:idx_evaluation_runs_tenant_created,priority:2,sort:desc;index:idx_evaluation_runs_tenant_config_created,priority:3,sort:desc"`
	UpdatedAt                time.Time           `gorm:"column:updated_at;not null;index:idx_evaluation_runs_tenant_status_updated,priority:3,sort:desc"`
	Revision                 uint64              `gorm:"column:revision;not null;default:1"`
	// Lease fields make recovery safe when more than one app instance is running.
	// A non-expired lease belongs to the instance that is actively evaluating the run.
	OwnerID     string     `gorm:"column:owner_id;type:varchar(128);not null;default:''"`
	LeaseUntil  *time.Time `gorm:"column:lease_until"`
	HeartbeatAt *time.Time `gorm:"column:heartbeat_at"`
}

// TableName pins the table used by the evaluation repository.
func (EvaluationRunRecord) TableName() string { return "evaluation_runs" }

// EvaluationRunCaseRecord stores the latest observable result for one dataset case.
type EvaluationRunCaseRecord struct {
	RunID            string              `gorm:"column:run_id;type:varchar(255);primaryKey"`
	CaseID           string              `gorm:"column:case_id;type:varchar(128);primaryKey"`
	TenantID         uint64              `gorm:"column:tenant_id;not null;index:idx_evaluation_run_cases_tenant_run,priority:1"`
	Status           EvaluationRunStatus `gorm:"column:status;type:varchar(16);not null"`
	StartedAt        time.Time           `gorm:"column:started_at;not null"`
	CompletedAt      *time.Time          `gorm:"column:completed_at"`
	DurationMS       int64               `gorm:"column:duration_ms;not null;default:0"`
	UsageSnapshot    JSON                `gorm:"column:usage_snapshot;type:json;not null"`
	WarningsSnapshot JSON                `gorm:"column:warnings_snapshot;type:json;not null"`
	ResultSnapshot   JSON                `gorm:"column:result_snapshot;type:json;not null"`
	CreatedAt        time.Time           `gorm:"column:created_at;not null"`
	UpdatedAt        time.Time           `gorm:"column:updated_at;not null"`
}

// TableName pins the table used by per-case evaluation observations.
func (EvaluationRunCaseRecord) TableName() string { return "evaluation_run_cases" }
