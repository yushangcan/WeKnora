"""Fail-closed checks for the CI harness, plus the provider's HTTP contracts."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

import run
import stub_provider as stub


def report_fixture():
    cases = [{"case_id": case_id, "status": "success", "evidence": {
        "ground_truth_pids": [pid], "metric_input_pids": [pid], "unmapped_result_count": 0,
        "generated_answer_fingerprint": "sha256:answer", "reference_answer_fingerprint": "sha256:answer"}}
        for case_id, pid in run.EXPECTED_CASES.items()]
    return {"task": {"status": 2, "total": 2, "finished": 2}, "config": {
        "runtime": {"commit_sha": "a" * 40, "commit_available": True},
        "dataset": {"corpus_count": 3, "case_count": 2, "files": [1] * 5,
                    "content_fingerprint": "sha256:fixture"}},
        "result": {"run": {"status": "success"}, "cases": cases,
                   "cost": {"amount": None},
                   "usage": {"calls": {"total": 3}, "reported_call_count": 1},
                   "retrieval": {"recall": 1.0, "mrr": 1.0, "map": 1.0}}}


class EvidenceTest(unittest.TestCase):
    def test_retrieval_degradation_requires_missing_relevant_pids_and_zero_recall(self):
        report = report_fixture()
        for case in report["result"]["cases"]:
            case["evidence"]["metric_input_pids"] = []
        report["result"]["retrieval"]["recall"] = 0.0
        report["result"]["usage"]["reported_call_count"] = 0
        run.validate_report(report, "a" * 40, positive=False, retrieval_positive=False)
        report["result"]["retrieval"]["recall"] = 1.0
        with self.assertRaisesRegex(RuntimeError, "lower recall"):
            run.validate_report(report, "a" * 40, positive=False, retrieval_positive=False)

    def test_complete_report_passes(self):
        run.validate_report(report_fixture(), "a" * 40)

    def test_invalid_reports_fail_closed(self):
        mutations = {
            "wrong revision": lambda r: r["config"]["runtime"].update(commit_sha="b" * 40),
            "unavailable revision": lambda r: r["config"]["runtime"].update(commit_available=False),
            "partial result": lambda r: r["result"]["run"].update(status="partial"),
            "missing cases": lambda r: r["result"]["cases"].pop(),
            "duplicate cases": lambda r: r["result"]["cases"][1].update(case_id="1"),
            "unmapped pid": lambda r: r["result"]["cases"][0]["evidence"].update(unmapped_result_count=1),
            "zero quality": lambda r: r["result"]["retrieval"].update(recall=0),
            "nan quality": lambda r: r["result"]["retrieval"].update(recall=float("nan")),
            "missing usage": lambda r: r["result"]["usage"].update(reported_call_count=0),
            "fabricated money": lambda r: r["result"]["cost"].update(amount=0),
            "wrong answer": lambda r: r["result"]["cases"][0]["evidence"].update(generated_answer_fingerprint="wrong"),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                report = copy.deepcopy(report_fixture())
                mutate(report)
                with self.assertRaises(RuntimeError):
                    run.validate_report(report, "a" * 40)


class DatasetEvidenceTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        files = []
        combined = bytearray()
        for name in run.DATASET_FILES:
            raw = ("synthetic bytes for " + name).encode()
            (self.directory / name).write_bytes(raw)
            files.append({"name": name, "size": len(raw),
                          "fingerprint": "sha256:" + hashlib.sha256(raw).hexdigest()})
            combined.extend(name.encode() + raw)
        self.dataset = {"files": files,
                        "content_fingerprint": "sha256:" + hashlib.sha256(combined).hexdigest()}

    def test_manifest_matches_actual_files(self):
        run.validate_dataset(self.dataset, self.directory)

    def test_invalid_manifests_fail_closed(self):
        mutations = {
            "duplicate file": lambda d: d["files"].__setitem__(1, d["files"][0]),
            "missing file": lambda d: d["files"].pop(),
            "wrong order": lambda d: d["files"].reverse(),
            "unexpected path": lambda d: d["files"][0].update(name="../queries.parquet"),
            "wrong size": lambda d: d["files"][0].update(size=0),
            "wrong file hash": lambda d: d["files"][0].update(fingerprint="sha256:wrong"),
            "wrong combined hash": lambda d: d.update(content_fingerprint="sha256:wrong"),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                dataset = copy.deepcopy(self.dataset)
                mutate(dataset)
                with self.assertRaises(RuntimeError):
                    run.validate_dataset(dataset, self.directory)

    def test_modified_file_fails_closed(self):
        path = self.directory / "corpus.parquet"
        raw = path.read_bytes()
        path.write_bytes(b"X" + raw[1:])  # Same size, different content.
        with self.assertRaisesRegex(RuntimeError, "file fingerprint mismatch"):
            run.validate_dataset(self.dataset, self.directory)


class ProviderTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), stub.Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.url = "http://127.0.0.1:" + str(cls.server.server_port)

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=2)

    def post(self, path, body, auth=True):
        req = urllib.request.Request(self.url + path, data=json.dumps(body).encode(),
              headers={"Content-Type": "application/json",
                       **({"Authorization": "Bearer ci-stub-key"} if auth else {})})
        return urllib.request.urlopen(req, timeout=3)

    def test_authentication_and_unknown_model(self):
        for auth, model, expected in ((False, "ci-chat", 401), (True, "unexpected", 400)):
            with self.assertRaises(urllib.error.HTTPError) as caught:
                self.post("/v1/chat/completions", {"model": model}, auth)
            self.assertEqual(caught.exception.code, expected)

    def test_embeddings_preserve_batch_order_and_distinguish_distractor(self):
        with self.post("/v1/embeddings", {"model": "ci-embedding", "input": [
                "What is the capital of Japan?", "Mars is a planet.", "Tokyo is the capital of Japan."]}) as response:
            result = json.load(response)
        self.assertEqual([d["index"] for d in result["data"]], [0, 1, 2])
        a, b, c = [d["embedding"] for d in result["data"]]
        self.assertEqual(len(a), 8)
        self.assertEqual(a, c)
        self.assertNotEqual(a, b)
        self.assertEqual(sum(x*x for x in a), 1)

    def test_rerank_preserves_indices(self):
        with self.post("/v1/rerank", {"model": "ci-rerank", "query": "capital of France",
                "documents": ["Mars is a planet.", "Paris is the capital of France."]}) as response:
            result = json.load(response)["results"]
        self.assertEqual(result[0]["index"], 1)
        self.assertGreater(result[0]["relevance_score"], 0.3)
        self.assertLess(result[1]["relevance_score"], 0.3)

    def test_chat_and_stream_usage(self):
        messages = [{"role": "user", "content": "What is the capital of France?"}]
        with self.post("/v1/chat/completions", {"model": "ci-chat", "messages": messages}) as response:
            result = json.load(response)
        self.assertEqual(result["choices"][0]["message"]["content"], "Paris")
        with self.post("/v1/chat/completions", {"model": "ci-chat", "messages": messages,
                       "stream": True}) as response:
            stream = response.read().decode()
        self.assertIn('"content": "Paris"', stream)
        self.assertIn('"total_tokens": 20', stream)
        self.assertTrue(stream.endswith("data: [DONE]\n\n"))

    def test_degraded_response_changes_answer(self):
        messages = [{"role": "user", "content": "What is the capital of Japan?"}]
        self.assertEqual(stub.answer(messages, "normal"), "Tokyo")
        self.assertEqual(stub.answer(messages, "degraded"), "Incorrect")

    def test_retrieval_degradation_filters_relevant_documents(self):
        try:
            with self.post("/control", {"mode": "degraded-retrieval"}):
                pass
            with self.post("/v1/rerank", {"model": "ci-rerank", "query": "capital of France",
                    "documents": ["Paris is the capital of France."]}) as response:
                result = json.load(response)["results"]
            self.assertEqual(result[0]["index"], 0)
            self.assertLess(result[0]["relevance_score"], 0.3)
        finally:
            with self.post("/control", {"mode": "normal"}):
                pass


if __name__ == "__main__":
    unittest.main()
