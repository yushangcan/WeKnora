#!/usr/bin/env python3
"""Exercise the real HTTP/CLI/persistence contracts in the isolated CI stack."""
import json
import hashlib
import math
import os
from pathlib import Path
import subprocess
import sys
import shutil
import urllib.error
import urllib.request

BASE_URL = "http://app:8080"
STUB_URL = "http://stub-provider:8090"
EVIDENCE = Path("/evidence")
CLI = "/build/evaluation"
EXPECTED_CASES = {"1": 10, "2": 20}
DATASET_FILES = ("queries.parquet", "corpus.parquet", "qrels.parquet", "qas.parquet", "answers.parquet")
# Valid only in this disposable stack. Never accept a production URL/token.
IDENTITY = {"username": "evaluation-ci", "email": "evaluation-ci@example.com",
            "password": "EphemeralCi123"}
METRICS = "precision,recall,ndcg3,ndcg10,mrr,map,bleu1,bleu2,bleu4,rouge1,rouge2,rougel"


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def write_json(name, value):
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    (EVIDENCE / name).write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def request(path, body=None, headers=None, expected_status=200, base=BASE_URL):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + path, data=data, headers={
        "Content-Type": "application/json", **(headers or {})})
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            status, raw = response.status, response.read()
    except urllib.error.HTTPError as error:
        status, raw = error.code, error.read()
    require(status == expected_status, f"HTTP {path}: expected {expected_status}, got {status}")
    return json.loads(raw)


def authenticate(register=False):
    if register:
        require(request("/api/v1/auth/register", IDENTITY, expected_status=201)["success"], "registration failed")
    login = request("/api/v1/auth/login", {"email": IDENTITY["email"], "password": IDENTITY["password"]})
    require(login.get("success"), "login failed")
    tenant = login["active_tenant"]["id"]
    require(tenant > 0, "active tenant missing")
    key = request(f"/api/v1/tenants/{tenant}/api-keys",
                  {"name": "evaluation-ci", "capabilities": ["run_evaluations"]},
                  {"Authorization": "Bearer " + login["token"]}, 201)
    require(key.get("success") and key["data"].get("token"), "evaluation API key missing")
    return {"X-API-Key": key["data"]["token"], "X-Tenant-ID": str(tenant)}


def cli_env(headers):
    return {**os.environ, "WEKNORA_BASE_URL": BASE_URL, "WEKNORA_API_KEY": headers["X-API-Key"],
            "WEKNORA_TENANT_ID": headers["X-Tenant-ID"], "EVALUATION_DATASET_ID": "default",
            "EVALUATION_CHAT_MODEL_ID": "ci-chat", "EVALUATION_RERANK_MODEL_ID": "ci-rerank",
            "EVALUATION_POLL_INTERVAL": "250ms", "EVALUATION_TIMEOUT": "3m",
            "EVALUATION_QUALITY_TOLERANCE": "0", "EVALUATION_GATE_METRICS": METRICS}


def run_cli(name, env, command=None, expect_failure=False, expected_metric=None):
    with (EVIDENCE / f"{name}.log").open("w", encoding="utf-8") as log:
        result = subprocess.run([CLI] + ([command] if command else []),
                                env=env, stdout=log, stderr=subprocess.STDOUT, timeout=210)
    output = (EVIDENCE / f"{name}.log").read_text(encoding="utf-8")
    if expect_failure:
        require(result.returncode != 0 and "quality gate failed" in output,
                "degraded result did not trigger the quality gate")
        if expected_metric:
            require(any("quality regression" in line and expected_metric in line
                        for line in output.splitlines()),
                    f"quality gate did not identify {expected_metric}")
    else:
        require(result.returncode == 0, f"{name} failed (see {name}.log)")
    print(f"{name}: {'rejected as expected' if expect_failure else 'passed'}", flush=True)


def read_report(name):
    return json.loads((EVIDENCE / f"{name}.json").read_text(encoding="utf-8"))["data"]


def validate_report(detail, commit, positive=True, retrieval_positive=True):
    task, config, result = detail["task"], detail["config"], detail["result"]
    require(task["status"] == 2 and result["run"]["status"] == "success", "run did not succeed")
    require(task["total"] == task["finished"] == 2, "run did not finish exactly two cases")
    runtime = config["runtime"]
    require(runtime["commit_available"] and runtime["commit_sha"] == commit,
            "evaluated binary revision does not match checkout")
    require(config["dataset"]["corpus_count"] == 3 and config["dataset"]["case_count"] == 2,
            "fixture must include two cases and a distractor")
    require(len(config["dataset"]["files"]) == 5 and
            config["dataset"]["content_fingerprint"].startswith("sha256:"), "dataset manifest missing")
    cases = result["cases"]
    require(len(cases) == 2 and {c["case_id"] for c in cases} == set(EXPECTED_CASES),
            "case evidence missing or duplicated")
    for case in cases:
        evidence = case["evidence"]
        require(case["status"] == "success", "case is incomplete")
        require(evidence["unmapped_result_count"] == 0, "retrieved PID was not preserved")
        require(evidence["ground_truth_pids"] == [EXPECTED_CASES[case["case_id"]]], "incorrect ground truth")
        if retrieval_positive:
            require(evidence["metric_input_pids"] == evidence["ground_truth_pids"], "retrieval did not isolate relevant passage")
        else:
            require(not set(evidence.get("metric_input_pids") or []) & set(evidence["ground_truth_pids"]),
                    "retrieval degradation still returned the relevant passage")
        if positive:
            require(evidence["generated_answer_fingerprint"] == evidence["reference_answer_fingerprint"],
                    "synthetic answer does not match reference")
    require(result["usage"]["calls"]["total"] > 0, "usage observation missing")
    # An empty rerank takes the App's no-context fallback and skips generation.
    # This generic embedding/rerank adapter records calls but need not expose
    # token counts. Do not demand or fabricate chat usage on that branch.
    if retrieval_positive:
        require(result["usage"]["reported_call_count"] > 0, "usage observation missing")
    require(result["cost"]["amount"] is None, "synthetic provider must not invent monetary cost")
    if not retrieval_positive:
        require(result["retrieval"]["recall"] == 0.0, "retrieval degradation did not lower recall to zero")
    if positive:
        for metric in ("recall", "mrr", "map"):
            value = result["retrieval"][metric]
            require(math.isfinite(value) and abs(value - 1.0) < 1e-9, f"{metric} missed the fixture quality floor")


def history(headers, run_id, name):
    overview = request(f"/api/v1/evaluation/runs/{run_id}", headers=headers)
    cases = request(f"/api/v1/evaluation/runs/{run_id}/cases?page=1&page_size=100", headers=headers)
    write_json(f"{name}-run.json", overview)
    write_json(f"{name}-cases.json", cases)
    summary = overview["data"]["summary"]
    require(summary["run_id"] == run_id and summary["status"] == "success", "history run mismatch")
    require(summary["progress"]["cases"]["total"] == summary["progress"]["cases"]["success"] == 2,
            "persisted case counts mismatch")
    items = cases["data"]["items"]
    require(cases["data"]["total"] == len(items) == 2 and
            {c["case_id"] for c in items} == set(EXPECTED_CASES), "persisted cases missing or duplicated")
    for case in items:
        require(case["status"] == "success", "persisted case failed")
    return overview


def validate_dataset(dataset, directory=Path("/build/runtime/dataset/samples")):
    # Verify reported hashes against the bytes actually mounted into the app.
    # Match the loader's full ordered manifest before resolving any paths.
    require(tuple(entry["name"] for entry in dataset["files"]) == DATASET_FILES,
            "dataset manifest must contain each expected file once in loader order")
    dataset_hash = hashlib.sha256()
    for entry in dataset["files"]:
        raw = (directory / entry["name"]).read_bytes()
        require(entry["size"] == len(raw) and entry["fingerprint"] == "sha256:" + hashlib.sha256(raw).hexdigest(),
                "dataset file fingerprint mismatch")
        dataset_hash.update(entry["name"].encode())
        dataset_hash.update(raw)
    require(dataset["content_fingerprint"] == "sha256:" + dataset_hash.hexdigest(),
            "dataset fingerprint does not describe the mounted fixture")


def evaluate(headers, env, name, commit, positive=True, retrieval_positive=True):
    run_cli(name, {**env, "EVALUATION_REPORT_PATH": str(EVIDENCE / f"{name}.json")})
    report = read_report(name)
    validate_report(report, commit, positive, retrieval_positive)
    validate_dataset(report["config"]["dataset"])
    run_id = report["task"]["id"]
    history(headers, run_id, name)
    return report


def compare(headers, env, baseline_id, candidate_id, name, failure=False, expected_metric=None):
    env = {**env, "EVALUATION_BASELINE_RUN_ID": baseline_id,
           "EVALUATION_COMPARISON_RUN_IDS": baseline_id + "," + candidate_id,
           "EVALUATION_COMPARISON_REPORT_PATH": str(EVIDENCE / f"{name}.json")}
    run_cli(name, env, "compare")
    run_cli(name + "-gate", env, "gate", failure, expected_metric)


def run(commit):
    request("/api/v1/evaluation/runs", expected_status=401)
    headers = authenticate(register=True)
    env = cli_env(headers)
    baseline = evaluate(headers, env, "baseline", commit)
    candidate = evaluate(headers, env, "candidate", commit)
    baseline_id, candidate_id = baseline["task"]["id"], candidate["task"]["id"]
    require(baseline_id != candidate_id, "run IDs collided")
    require(baseline["config"]["config_hash"] == candidate["config"]["config_hash"],
            "repeat runs changed the effective configuration")
    compare(headers, env, baseline_id, candidate_id, "comparison")
    # A third real evaluation completes normally but produces wrong answers.
    # Its persisted quality delta must cause the CLI gate to exit nonzero.
    control_headers = {"Authorization": "Bearer ci-stub-key"}
    request("/control", {"mode": "degraded"}, control_headers, base=STUB_URL)
    try:
        degraded = evaluate(headers, env, "degraded", commit, positive=False)
    finally:
        request("/control", {"mode": "normal"}, control_headers, base=STUB_URL)
    degraded_id = degraded["task"]["id"]
    require(degraded_id not in (baseline_id, candidate_id), "degraded run ID collided")
    compare(headers, env, baseline_id, degraded_id, "regression", failure=True)
    # Drop all relevant rerank scores below the real pipeline's filter threshold.
    # The unchanged dataset/config must yield zero recall and a metric-specific failure.
    request("/control", {"mode": "degraded-retrieval"}, control_headers, base=STUB_URL)
    try:
        retrieval = evaluate(headers, env, "retrieval-degraded", commit,
                             positive=False, retrieval_positive=False)
    finally:
        request("/control", {"mode": "normal"}, control_headers, base=STUB_URL)
    retrieval_id = retrieval["task"]["id"]
    require(len({baseline_id, candidate_id, degraded_id, retrieval_id}) == 4, "run IDs collided")
    compare(headers, env, baseline_id, retrieval_id, "retrieval-regression",
            failure=True, expected_metric="recall")
    stats = request("/stats", base=STUB_URL)
    write_json("provider-stats.json", stats)
    require(all(stats.get(k, 0) > 0 for k in ("chat", "embedding", "rerank")),
            "a provider operation was skipped")
    write_json("runs.json", {"tenant_id": int(headers["X-Tenant-ID"]),
        "baseline": baseline_id, "candidate": candidate_id, "degraded": degraded_id,
        "retrieval-degraded": retrieval_id,
        "commit": commit, "provider": "synthetic-ci-v1"})
    write_json("run-status.json", {"status": "passed", "commit": commit, "runs": 4, "cases": 8,
        "positive_gate": "passed", "negative_gate": "rejected", "provider": "synthetic-ci-v1",
        "retrieval_negative_gate": "rejected", "degraded_recall": retrieval["result"]["retrieval"]["recall"],
        "vcs_modified": candidate["config"]["runtime"]["vcs_modified"]})


def verify_restart(commit):
    headers = authenticate()
    env = cli_env(headers)
    ids = json.loads((EVIDENCE / "runs.json").read_text())
    require(ids["commit"] == commit, "restart checked a different checkout")
    for name in ("baseline", "candidate", "degraded", "retrieval-degraded"):
        before = read_report(name)
        after = request("/api/v1/evaluation?task_id=" + ids[name], headers=headers)
        write_json(f"restart-{name}.json", after)
        validate_report(after["data"], commit, positive=name in ("baseline", "candidate"),
                        retrieval_positive=name != "retrieval-degraded")
        require(after["data"]["result"] == before["result"], "persisted result changed after app restart")
        history(headers, ids[name], f"restart-{name}")
    compare(headers, env, ids["baseline"], ids["candidate"], "restart-comparison")
    compare(headers, env, ids["baseline"], ids["retrieval-degraded"], "restart-retrieval-regression",
            failure=True, expected_metric="recall")
    write_json("restart-status.json", {"status": "passed", "runs": 4, "cases": 8})


if __name__ == "__main__":
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    phase = sys.argv[1] if len(sys.argv) > 1 else "run"
    try:
        require(phase in ("run", "verify-restart"), "unsupported phase")
        commit = os.environ["EVALUATION_EXPECTED_COMMIT"]
        require(len(commit) == 40 and all(c in "0123456789abcdef" for c in commit),
                "expected commit must be a full Git SHA")
        for name in ("source-commit.txt", "build-info.txt"):
            shutil.copyfile(Path("/build") / name, EVIDENCE / name)
        (run if phase == "run" else verify_restart)(commit)
    except Exception as error:
        status_name = "restart" if phase == "verify-restart" else "run"
        write_json(f"{status_name}-status.json", {"status": "failed", "error": str(error)})
        print(f"{phase}: {error}", file=sys.stderr)
        sys.exit(1)
