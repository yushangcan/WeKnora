package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// versionedSQLiteTables is the set of tables that SQLite migrations must
// create to stay in sync with the versioned (PostgreSQL) migrations:
// 000041 task queue, 000053 system settings, 000055 processing spans,
// 000063 knowledge multi-tags, 000091 evaluation persistence, 000092 model
// usage events, 000093 provider request identity, 000094 cost source, and
// 000095 evaluation run leases.
var versionedSQLiteTables = []string{
	"task_pending_ops",
	"task_dead_letters",
	"system_settings",
	"knowledge_processing_spans",
	"knowledge_tag_relations",
	"evaluation_runs",
	"evaluation_run_cases",
	"model_usage_events",
}

// versionedSQLiteColumns maps each existing table to the columns that the
// versioned migrations add and the SQLite baseline was missing.
var versionedSQLiteColumns = map[string][]string{
	"tenants":            {"api_principal_config"},           // 000064
	"users":              {"is_system_admin"},                // 000053
	"knowledges":         {"pending_subtasks_count"},         // 000056
	"messages":           {"attachments", "usage"},           // 000034, 000085
	"tenant_invitations": {"token", "accepted_count"},        // 000054
	"embed_channels":     {"allow_memory"},                   // 000060
	"mcp_oauth_tokens":   {"principal_type", "principal_id"}, // 000064
}

const expectedSQLiteMigrationVersion = 17

func TestSQLiteMigrationsCreateVersionedSchema(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	chdirAndRestore(t, repoRoot)

	dbPath := filepath.Join(t.TempDir(), "fresh.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))

	db := openSQLiteDB(t, dbPath)
	version, dirty := sqliteMigrationState(t, db)
	require.Equal(t, expectedSQLiteMigrationVersion, version)
	require.False(t, dirty)

	for _, table := range versionedSQLiteTables {
		require.Truef(t, sqliteTableExists(t, db, table), "SQLite migrations must create table %s", table)
	}
	for table, columns := range versionedSQLiteColumns {
		for _, column := range columns {
			require.Truef(
				t,
				sqliteColumnExists(t, db, table, column),
				"SQLite migrations must add column %s.%s",
				table,
				column,
			)
		}
	}

	assertSQLiteShareLinkInvitationsWork(t, db)
	assertSQLiteMCPOAuthPrincipalUpsertWorks(t, db)
	assertSQLiteEvaluationSchemaWorks(t, db)
	assertSQLiteModelUsageSchemaWorks(t, db)
	require.False(t, sqliteColumnExists(t, db, "knowledges", "tag_id"),
		"SQLite migrations must drop legacy knowledges.tag_id after multi-tag migration")
}

func TestSQLiteMigrationsUpgradeV11PreservesData(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	legacyRoot := copySQLiteMigrationsThrough(t, repoRoot, 11)
	chdirAndRestore(t, legacyRoot)

	dbPath := filepath.Join(t.TempDir(), "upgrade-v11.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	db := openSQLiteDB(t, dbPath)
	versionBefore, dirtyBefore := sqliteMigrationState(t, db)
	require.Equal(t, 11, versionBefore)
	require.False(t, dirtyBefore)
	_, err := db.Exec("INSERT INTO tenants (name, business) VALUES (?, ?)", "v11-sentinel", "evaluation-upgrade-test")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	chdirAndRestore(t, repoRoot)
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	db = openSQLiteDB(t, dbPath)
	versionAfter, dirtyAfter := sqliteMigrationState(t, db)
	require.Equal(t, expectedSQLiteMigrationVersion, versionAfter)
	require.False(t, dirtyAfter)

	var sentinelName string
	require.NoError(t, db.QueryRow(
		"SELECT name FROM tenants WHERE business = ?", "evaluation-upgrade-test",
	).Scan(&sentinelName))
	require.Equal(t, "v11-sentinel", sentinelName)
	assertSQLiteEvaluationSchemaWorks(t, db)
	assertSQLiteModelUsageSchemaWorks(t, db)
}

func TestSQLiteEvaluationMigrationDownRemovesTables(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	chdirAndRestore(t, repoRoot)
	dbPath := filepath.Join(t.TempDir(), "evaluation-down.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	db := openSQLiteDB(t, dbPath)

	downSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "sqlite", "000013_evaluation_runs.down.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(downSQL))
	require.NoError(t, err)
	require.False(t, sqliteTableExists(t, db, "evaluation_run_cases"))
	require.False(t, sqliteTableExists(t, db, "evaluation_runs"))
}

func TestPostgresEvaluationMigrationContract(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	upSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "versioned", "000091_evaluation_runs.up.sql"))
	require.NoError(t, err)
	up := string(upSQL)
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS evaluation_runs",
		"CREATE TABLE IF NOT EXISTS evaluation_run_cases",
		"config_snapshot JSONB NOT NULL",
		"result_snapshot JSONB NOT NULL",
		"FOREIGN KEY (run_id) REFERENCES evaluation_runs(run_id) ON DELETE CASCADE",
		"idx_evaluation_runs_tenant_config_created",
		"idx_evaluation_run_cases_tenant_run",
	} {
		require.Contains(t, up, fragment)
	}
	downSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "versioned", "000091_evaluation_runs.down.sql"))
	require.NoError(t, err)
	require.Contains(t, string(downSQL), "DROP TABLE IF EXISTS evaluation_run_cases")
	require.Contains(t, string(downSQL), "DROP TABLE IF EXISTS evaluation_runs")
}

func TestPostgresModelUsageMigrationContract(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	upSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "versioned", "000092_model_usage_events.up.sql"))
	require.NoError(t, err)
	up := string(upSQL)
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS model_usage_events",
		"call_id VARCHAR(64) NOT NULL UNIQUE",
		"tenant_id BIGINT NOT NULL",
		"prompt_tokens BIGINT",
		"cache_status VARCHAR(16) NOT NULL",
		"cost_amount DOUBLE PRECISION",
		"idx_model_usage_tenant_started",
		"idx_model_usage_evaluation_run",
	} {
		require.Contains(t, up, fragment)
	}
	downSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "versioned", "000092_model_usage_events.down.sql"))
	require.NoError(t, err)
	require.Contains(t, string(downSQL), "DROP TABLE IF EXISTS model_usage_events")
}

func TestPostgresModelUsageProviderRequestMigrationContract(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	upSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "versioned", "000093_model_usage_provider_request_id.up.sql"))
	require.NoError(t, err)
	up := string(upSQL)
	require.Contains(t, up, "ADD COLUMN IF NOT EXISTS provider_request_id VARCHAR(255)")
	require.Contains(t, up, "idx_model_usage_provider_request")
	downSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "versioned", "000093_model_usage_provider_request_id.down.sql"))
	require.NoError(t, err)
	require.Contains(t, string(downSQL), "DROP COLUMN IF EXISTS provider_request_id")
}

func TestPostgresModelUsageCostSourceMigrationContract(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	upSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "versioned", "000094_model_usage_cost_source.up.sql"))
	require.NoError(t, err)
	require.Contains(t, string(upSQL), "ADD COLUMN IF NOT EXISTS cost_source VARCHAR(32)")
	downSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "versioned", "000094_model_usage_cost_source.down.sql"))
	require.NoError(t, err)
	require.Contains(t, string(downSQL), "DROP COLUMN IF EXISTS cost_source")
}

func TestPostgresEvaluationLeaseMigrationContract(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	upSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "versioned", "000095_evaluation_run_leases.up.sql"))
	require.NoError(t, err)
	for _, fragment := range []string{
		"owner_id VARCHAR(128) NOT NULL DEFAULT ''",
		"lease_until TIMESTAMP WITH TIME ZONE",
		"heartbeat_at TIMESTAMP WITH TIME ZONE",
		"idx_evaluation_runs_lease",
	} {
		require.Contains(t, string(upSQL), fragment)
	}
	downSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "versioned", "000095_evaluation_run_leases.down.sql"))
	require.NoError(t, err)
	for _, column := range []string{"owner_id", "lease_until", "heartbeat_at"} {
		require.Contains(t, string(downSQL), "DROP COLUMN IF EXISTS "+column)
	}
}

func TestSQLiteEvaluationLeaseMigrationRollbackPreservesRun(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	chdirAndRestore(t, repoRoot)
	dbPath := filepath.Join(t.TempDir(), "leases.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	db := openSQLiteDB(t, dbPath)
	_, err := db.Exec(`INSERT INTO evaluation_runs (
		run_id, tenant_id, dataset_id, dataset_version, dataset_fingerprint, config_hash,
		embedding_model_id, chat_model_id, status, config_snapshot, params_snapshot,
		result_snapshot, started_at, owner_id
	) VALUES ('lease-sentinel', 7, 'dataset', '1', 'fingerprint', 'config', 'embedding', 'chat',
		'running', '{}', '{}', '{}', CURRENT_TIMESTAMP, 'owner')`)
	require.NoError(t, err)
	downSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "sqlite", "000017_evaluation_run_leases.down.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(downSQL))
	require.NoError(t, err)
	require.False(t, sqliteColumnExists(t, db, "evaluation_runs", "owner_id"))
	require.False(t, sqliteIndexExists(t, db, "idx_evaluation_runs_lease"))
	upSQL, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "sqlite", "000017_evaluation_run_leases.up.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(upSQL))
	require.NoError(t, err)
	var owner, status string
	require.NoError(t, db.QueryRow("SELECT owner_id, status FROM evaluation_runs WHERE run_id='lease-sentinel'").Scan(&owner, &status))
	require.Empty(t, owner)
	require.Equal(t, "running", status)
	assertSQLiteEvaluationSchemaWorks(t, db)
}

func TestSQLiteMigrationsUpgradeV4PreservesData(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)

	// Build a legacy v4 migration root (000000_init .. 000004_memory) so we
	// can prove the new migrations upgrade an existing Lite database without
	// replaying the baseline.
	legacyRoot := copySQLiteMigrationsV4(t, repoRoot)
	chdirAndRestore(t, legacyRoot)

	dbPath := filepath.Join(t.TempDir(), "upgrade.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))

	db := openSQLiteDB(t, dbPath)
	versionBefore, dirtyBefore := sqliteMigrationState(t, db)
	require.Equal(t, 4, versionBefore)
	require.False(t, dirtyBefore)
	_, err := db.Exec("INSERT INTO tenants (name, business) VALUES (?, ?)", "upgrade-sentinel", "migration-test")
	require.NoError(t, err)
	_, err = db.Exec(
		"INSERT INTO knowledges (id, tenant_id, knowledge_base_id, type, title, source, tag_id) "+
			"VALUES (?, 1, ?, 'document', 'tagged-doc', 'manual', ?)",
		"legacy-knowledge-1", "legacy-kb-1", "legacy-tag-1",
	)
	require.NoError(t, err)

	// Run the full migration set from the repo root.
	chdirAndRestore(t, repoRoot)
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))

	db = openSQLiteDB(t, dbPath)
	versionAfter, dirtyAfter := sqliteMigrationState(t, db)
	require.Equal(t, expectedSQLiteMigrationVersion, versionAfter)
	require.False(t, dirtyAfter)

	for _, table := range versionedSQLiteTables {
		require.Truef(t, sqliteTableExists(t, db, table), "upgraded SQLite DB must have table %s", table)
	}
	for table, columns := range versionedSQLiteColumns {
		for _, column := range columns {
			require.Truef(
				t,
				sqliteColumnExists(t, db, table, column),
				"upgraded SQLite DB must have column %s.%s",
				table,
				column,
			)
		}
	}
	assertSQLiteModelUsageSchemaWorks(t, db)

	var sentinelName string
	require.NoError(t, db.QueryRow("SELECT name FROM tenants WHERE business = ?", "migration-test").Scan(&sentinelName))
	require.Equal(t, "upgrade-sentinel", sentinelName)

	var relationCount int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM knowledge_tag_relations WHERE knowledge_id = ? AND tag_id = ?",
		"legacy-knowledge-1", "legacy-tag-1",
	).Scan(&relationCount))
	require.Equal(t, 1, relationCount)
	require.False(t, sqliteColumnExists(t, db, "knowledges", "tag_id"))
}

func sqliteRepoRoot(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return repoRoot
}

func chdirAndRestore(t *testing.T, dir string) {
	t.Helper()
	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(previousDir) })
}

func openSQLiteDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func sqliteMigrationState(t *testing.T, db *sql.DB) (version int, dirty bool) {
	t.Helper()
	require.NoError(t, db.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	return version, dirty
}

func sqliteTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		table,
	).Scan(&n))
	return n == 1
}

func sqliteColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?",
		table,
		column,
	).Scan(&n))
	return n == 1
}

func sqliteIndexExists(t *testing.T, db *sql.DB, index string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?",
		index,
	).Scan(&n))
	return n == 1
}

func assertSQLiteEvaluationSchemaWorks(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, column := range []string{"owner_id", "lease_until", "heartbeat_at"} {
		require.Truef(t, sqliteColumnExists(t, db, "evaluation_runs", column), "SQLite evaluation migration must add column %s", column)
	}
	require.True(t, sqliteIndexExists(t, db, "idx_evaluation_runs_lease"))
	for _, table := range []string{"evaluation_runs", "evaluation_run_cases"} {
		require.Truef(t, sqliteTableExists(t, db, table), "SQLite migrations must create table %s", table)
	}
	for _, index := range []string{
		"idx_evaluation_runs_tenant_created",
		"idx_evaluation_runs_tenant_status_updated",
		"idx_evaluation_runs_tenant_config_created",
		"idx_evaluation_run_cases_tenant_run",
	} {
		require.Truef(t, sqliteIndexExists(t, db, index), "SQLite migrations must create index %s", index)
	}

	var referencedTable, onDelete string
	rows, err := db.Query("PRAGMA foreign_key_list(evaluation_run_cases)")
	require.NoError(t, err)
	for rows.Next() {
		var id, seq int
		var from, to, onUpdate, match string
		require.NoError(t, rows.Scan(&id, &seq, &referencedTable, &from, &to, &onUpdate, &onDelete, &match))
		if referencedTable == "evaluation_runs" && from == "run_id" && to == "run_id" {
			break
		}
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Equal(t, "evaluation_runs", referencedTable)
	require.Equal(t, "CASCADE", onDelete)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	runInsert := `INSERT INTO evaluation_runs (
        run_id, tenant_id, dataset_id, dataset_version, dataset_fingerprint, config_hash,
        embedding_model_id, chat_model_id, status, config_snapshot, params_snapshot,
        result_snapshot, started_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = db.Exec(
		runInsert,
		"migration-run", 7, "default", "1", "sha256:dataset", "sha256:config",
		"embedding-1", "chat-1", "running", `{}`, `{}`, `{}`, "2026-08-26T00:00:00Z",
	)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO evaluation_run_cases (
        run_id, case_id, tenant_id, status, started_at, usage_snapshot, warnings_snapshot, result_snapshot
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"migration-run", "1", 7, "success", "2026-08-26T00:00:00Z", `{}`, `[]`, `{}`,
	)
	require.NoError(t, err)
	_, err = db.Exec("DELETE FROM evaluation_runs WHERE run_id = ?", "migration-run")
	require.NoError(t, err)
	var caseCount int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM evaluation_run_cases WHERE run_id = ?", "migration-run",
	).Scan(&caseCount))
	require.Equal(t, 0, caseCount)
}

func assertSQLiteModelUsageSchemaWorks(t *testing.T, db *sql.DB) {
	t.Helper()
	require.True(t, sqliteTableExists(t, db, "model_usage_events"))
	for _, column := range []string{
		"call_id", "tenant_id", "model_id", "model_name_snapshot", "model_type",
		"operation", "started_at", "success", "total_tokens", "cache_status",
		"cost_amount", "cost_source", "cost_status", "evaluation_run_id", "provider_request_id",
	} {
		require.Truef(t, sqliteColumnExists(t, db, "model_usage_events", column), "SQLite model usage migration must add column %s", column)
	}
	for _, index := range []string{
		"idx_model_usage_tenant_started",
		"idx_model_usage_tenant_model_started",
		"idx_model_usage_tenant_type_started",
		"idx_model_usage_tenant_success_started",
		"idx_model_usage_evaluation_run", "idx_model_usage_provider_request",
	} {
		require.Truef(t, sqliteIndexExists(t, db, index), "SQLite model usage migration must create index %s", index)
	}
}

func assertSQLiteShareLinkInvitationsWork(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec("INSERT INTO tenants (name, business) VALUES (?, ?)", "share-link-tenant", "share-link-test")
	require.NoError(t, err)

	expiresAt := "2099-01-01 00:00:00"
	shareLinkInsert := "INSERT INTO tenant_invitations " +
		"(tenant_id, invitee_user_id, token, role, status, expires_at) " +
		"VALUES (1, '', ?, 'member', 'pending', ?)"
	_, err = db.Exec(shareLinkInsert, "token-a", expiresAt)
	require.NoError(t, err)
	_, err = db.Exec(shareLinkInsert, "token-b", expiresAt)
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM tenant_invitations WHERE tenant_id = 1 AND invitee_user_id = '' AND status = 'pending'",
	).Scan(&count))
	require.Equal(t, 2, count)
}

func assertSQLiteMCPOAuthPrincipalUpsertWorks(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO mcp_services (id, tenant_id, name, transport_type) VALUES (?, 1, 'svc', 'http')",
		"svc-migration-1",
	)
	require.NoError(t, err)

	tokenInsertPrefix := "INSERT INTO mcp_oauth_tokens " +
		"(id, tenant_id, user_id, service_id, principal_type, principal_id, access_token) "
	_, err = db.Exec(
		tokenInsertPrefix +
			"VALUES ('tok-1', 1, 'u1', 'svc-migration-1', 'web_user', 'u1', 'token-1')",
	)
	require.NoError(t, err)

	_, err = db.Exec(
		tokenInsertPrefix +
			"VALUES ('tok-2', 1, 'u1', 'svc-migration-1', 'web_user', 'u1', 'token-2') " +
			"ON CONFLICT(tenant_id, principal_type, principal_id, service_id) " +
			"DO UPDATE SET access_token = excluded.access_token",
	)
	require.NoError(t, err)

	var accessToken string
	require.NoError(t, db.QueryRow(
		"SELECT access_token FROM mcp_oauth_tokens "+
			"WHERE tenant_id = 1 AND principal_type = 'web_user' "+
			"AND principal_id = 'u1' AND service_id = 'svc-migration-1'",
	).Scan(&accessToken))
	require.Equal(t, "token-2", accessToken)

	var rowCount int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM mcp_oauth_tokens WHERE tenant_id = 1 AND service_id = 'svc-migration-1'",
	).Scan(&rowCount))
	require.Equal(t, 1, rowCount)
}

func copySQLiteMigrationsV4(t *testing.T, repoRoot string) string {
	t.Helper()
	dest := t.TempDir()
	srcDir := filepath.Join(repoRoot, "migrations", "sqlite")
	destDir := filepath.Join(dest, "migrations", "sqlite")
	require.NoError(t, os.MkdirAll(destDir, 0o755))

	legacy := []string{
		"000000_init.up.sql",
		"000001_remove_wiki_log.up.sql",
		"000002_knowledge_folder_path.up.sql",
		"000003_knowledge_base_auto_tag_config.up.sql",
		"000004_memory.up.sql",
	}
	for _, name := range legacy {
		data, err := os.ReadFile(filepath.Join(srcDir, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(destDir, name), data, 0o600))
	}
	return dest
}

func copySQLiteMigrationsThrough(t *testing.T, repoRoot string, maxVersion int) string {
	t.Helper()
	dest := t.TempDir()
	srcDir := filepath.Join(repoRoot, "migrations", "sqlite")
	destDir := filepath.Join(dest, "migrations", "sqlite")
	require.NoError(t, os.MkdirAll(destDir, 0o755))

	entries, err := os.ReadDir(srcDir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		version, err := strconv.Atoi(entry.Name()[:6])
		require.NoError(t, err)
		if version > maxVersion {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, entry.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(destDir, entry.Name()), data, 0o600))
	}
	return dest
}
