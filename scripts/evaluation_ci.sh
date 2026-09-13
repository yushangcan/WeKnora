#!/usr/bin/env bash
# One local/Actions entrypoint. Requires Docker Compose v2, Bash and Git.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
export EVALUATION_EXPECTED_COMMIT
EVALUATION_EXPECTED_COMMIT="$(git rev-parse HEAD)"
# The CLI deliberately creates reports with mode 0600. Write them as the host
# caller so the Actions artifact uploader can read them without relaxing modes.
export EVALUATION_RUNNER_UID="$(id -u)"
export EVALUATION_RUNNER_GID="$(id -g)"
export EVALUATION_ARTIFACT_DIR="${EVALUATION_ARTIFACT_DIR:-./tmp/evaluation-ci/$(date -u +%Y%m%dT%H%M%SZ)-$$}"
mkdir -p "$EVALUATION_ARTIFACT_DIR"
# A unique project prevents both interference with production Compose and
# accidental reuse of a previous fixture database. No host ports are published.
project="weknora-eval-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-$$}"
dc=(docker compose --env-file ci/evaluation/compose.env -p "$project" -f docker-compose.evaluation.yml)
finish() {
  code=$?
  trap - EXIT
  set +e
  "${dc[@]}" logs --no-color app postgres redis stub-provider > "$EVALUATION_ARTIFACT_DIR/services.log" 2>&1
  "${dc[@]}" ps --all > "$EVALUATION_ARTIFACT_DIR/compose-ps.txt" 2>&1
  # Export only safe evaluation snapshots. Never dump users, auth tokens,
  # tenant keys, model credentials, or a developer's existing database.
  "${dc[@]}" exec -T postgres pg_dump -U weknora_ci -d weknora_ci \
    --no-owner --no-privileges --table=evaluation_runs --table=evaluation_run_cases \
    > "$EVALUATION_ARTIFACT_DIR/evaluation.sql" 2> "$EVALUATION_ARTIFACT_DIR/database-export.log"
  dump_code=$?
  if [[ $code -eq 0 && $dump_code -ne 0 ]]; then code=$dump_code; fi
  "${dc[@]}" down --volumes --remove-orphans > "$EVALUATION_ARTIFACT_DIR/cleanup.log" 2>&1
  cleanup_code=$?
  if [[ $code -eq 0 && $cleanup_code -ne 0 ]]; then code=$cleanup_code; fi
  printf 'exit_code=%s\ncommit=%s\nprovider=synthetic-ci-v1\n' "$code" "$EVALUATION_EXPECTED_COMMIT" > "$EVALUATION_ARTIFACT_DIR/status.txt"
  if [[ $code -eq 0 ]]; then
    echo "Evaluation contract passed. Evidence: $EVALUATION_ARTIFACT_DIR"
  else
    echo "Evaluation contract failed (exit $code). Evidence: $EVALUATION_ARTIFACT_DIR" >&2
  fi
  exit "$code"
}
trap finish EXIT
"${dc[@]}" config --quiet
"${dc[@]}" run --rm --no-deps build 2>&1 | tee "$EVALUATION_ARTIFACT_DIR/build.log"
"${dc[@]}" up -d --wait --wait-timeout 180 app
"${dc[@]}" run --rm runner run
"${dc[@]}" restart app
"${dc[@]}" up -d --wait --wait-timeout 120 app
"${dc[@]}" run --rm runner verify-restart
# Direct SQL complements HTTP history checks and verifies terminal lease release.
"${dc[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U weknora_ci -d weknora_ci \
  < ci/evaluation/assert_database.sql > "$EVALUATION_ARTIFACT_DIR/database-check.txt"
