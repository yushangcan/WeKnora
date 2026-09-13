DO $$
BEGIN
    IF (SELECT COUNT(*) FROM evaluation_runs) <> 4
       OR (SELECT COUNT(*) FROM evaluation_runs WHERE status = 'success' AND total = 2 AND finished = 2) <> 4 THEN
        RAISE EXCEPTION 'expected four completed evaluation runs';
    END IF;
    IF (SELECT COUNT(*) FROM evaluation_run_cases) <> 8
       OR (SELECT COUNT(*) FROM evaluation_run_cases WHERE status = 'success') <> 8 THEN
        RAISE EXCEPTION 'expected eight persisted successful cases';
    END IF;
    IF EXISTS (SELECT 1 FROM evaluation_runs WHERE owner_id <> '' OR lease_until IS NOT NULL) THEN
        RAISE EXCEPTION 'terminal run still owns an execution lease';
    END IF;
END $$;
SELECT run_id, status, total, finished, dataset_fingerprint,
       config_snapshot->'runtime'->>'commit_sha' AS commit_sha
FROM evaluation_runs ORDER BY created_at;
SELECT run_id, case_id, status FROM evaluation_run_cases ORDER BY run_id, case_id;
