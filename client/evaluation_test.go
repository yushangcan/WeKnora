package client

import (
	"encoding/json"
	"testing"
)

func TestEvaluationResultResponseMatchesServerContract(t *testing.T) {
	payload := []byte(`{
        "success": true,
        "data": {
            "task": {
                "id": "evaluation-1",
                "tenant_id": 9,
                "dataset_id": "default",
                "start_time": "2026-08-24T08:00:00Z",
                "status": 2,
                "total": 1,
                "finished": 1
            },
            "params": {},
            "config": {
                "schema_version": "evaluation-config/v1",
                "dataset": {
                    "id": "default",
                    "version": "1",
                    "content_fingerprint": "sha256:dataset",
                    "query_count": 1,
                    "corpus_count": 1,
                    "case_count": 1,
                    "ingestion_mode": "passage_chunking"
                },
                "models": {
                    "embedding": {"id": "embedding-1", "parameters_fingerprint": "sha256:embedding"},
                    "chat": {"id": "chat-1", "parameters_fingerprint": "sha256:chat"}
                },
                "chunking": {"applied": true, "source_unit": "dataset_passage", "config": {"chunk_size": 512}},
                "retrieval": {"embedding_top_k": 10},
                "generation": {"seed": 7},
                "indexing": {"vector_enabled": true},
                "runtime": {"case_concurrency": 2},
                "config_hash": "sha256:config"
            },
            "metric": {
                "retrieval_metrics": {"precision": 0.5},
                "generation_metrics": {"bleu1": 0.25}
            },
            "result": {
                "schema_version": "evaluation-run/v1",
                "run": {
                    "run_id": "evaluation-1",
                    "tenant_id": 9,
                    "dataset_id": "default",
                    "started_at": "2026-08-24T08:00:00Z",
                    "completed_at": "2026-08-24T08:00:01Z",
                    "status": "success"
                },
                "retrieval": {"precision": 0.5},
                "answer": {"bleu1": 0.25},
                "usage": {
                    "status": "partial",
                    "calls": {"total": 2, "succeeded": 2, "failed": 0, "items": 3},
                    "tokens": {"total_tokens": 8},
                    "cache_status": "unreported",
                    "reported_call_count": 1,
                    "unavailable_call_count": 1,
                    "by_model": [],
                    "by_phase": []
                },
                "cost": {
                    "status": "unavailable",
                    "source": "not_reported",
                    "amount": null,
                    "warnings": []
                },
                "timing": {"total_wall_time_ms": 1000},
                "cases": [],
                "warnings": []
            }
        }
    }`)

	var response EvaluationResultResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("unmarshal evaluation response: %v", err)
	}
	if response.Data.Task == nil || response.Data.Task.Status != "success" ||
		response.Data.Task.StatusCode != EvaluationStatusSuccess {
		t.Fatalf("task contract was not decoded: %#v", response.Data.Task)
	}
	if response.Data.Metric == nil || response.Data.Metric.Retrieval.Precision != 0.5 {
		t.Fatalf("legacy metric was not decoded: %#v", response.Data.Metric)
	}
	if response.Data.Config == nil || response.Data.Config.ConfigHash != "sha256:config" ||
		response.Data.Config.Dataset.ContentFingerprint != "sha256:dataset" {
		t.Fatalf("run configuration was not decoded: %#v", response.Data.Config)
	}
	if response.Data.Result == nil || response.Data.Result.Usage.Tokens.TotalTokens != 8 {
		t.Fatalf("four-dimension result was not decoded: %#v", response.Data.Result)
	}
	if response.Data.Result.Cost.Amount != nil {
		t.Fatalf("null cost became a value: %#v", response.Data.Result.Cost)
	}
}

func TestEvaluationTaskResponseAcceptsNestedServerTask(t *testing.T) {
	payload := []byte(`{
		"success": true,
		"data": {
			"task": {
				"id": "evaluation-1",
				"tenant_id": 9,
				"dataset_id": "default",
				"start_time": "2026-08-24T08:00:00Z",
				"status": 1
			},
			"params": {},
			"result": {
				"schema_version": "evaluation-run/v1",
				"run": {
					"run_id": "evaluation-1",
					"tenant_id": 9,
					"dataset_id": "default",
					"started_at": "2026-08-24T08:00:00Z",
					"completed_at": null,
					"status": "running"
				},
				"retrieval": null,
				"answer": null,
				"usage": {"by_model": [], "by_phase": []},
				"cost": {"amount": null, "warnings": []},
				"timing": {},
				"cases": [],
				"warnings": []
			}
		}
	}`)

	var response EvaluationTaskResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("unmarshal task response: %v", err)
	}
	if response.Data.ID != "evaluation-1" || response.Data.Status != "running" {
		t.Fatalf("nested task was not decoded: %#v", response.Data)
	}
}
