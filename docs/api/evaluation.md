# 评估功能 API

[返回目录](./README.md)

| 方法 | 路径 | 描述 |
| --- | --- | --- |
| GET | `/evaluation` | 按任务 ID 获取评测结果 |
| POST | `/evaluation` | 创建评测任务 |
| GET | `/evaluation/runs` | 分页查询历史 Run |
| GET | `/evaluation/runs/:run_id` | 获取单个 Run 概览与配置快照 |
| GET | `/evaluation/runs/:run_id/cases` | 独立分页查询 Case 证据 |
| GET | `/evaluation/comparison` | 比较 2 至 5 个持久化 Run |

> 注：服务端路由带尾斜杠（Gin 会自动从 `/evaluation` 重定向到 `/evaluation/`），下方示例为方便阅读用了 `/evaluation`。

> 当前实现会把任务状态、固定配置、四维结果和 Case 审计证据写入项目现有数据库，服务重启后仍可查询。当前配置契约为 `evaluation-config/v2`，检索与生成指标输入契约为 `retrieval-generation/v2`。历史分页、详情、Case 分页、跨 Run 对比和 Vue 页面已经接入同一套持久化快照。

## 一条命令执行评测

仓库提供 `make eval` 入口，负责创建评测、轮询任务状态并把最后一次完整 API 响应写入 JSON 报告。命令只从环境变量读取认证信息，API Key 不会写入报告或控制台输出。

PowerShell：

```powershell
$env:WEKNORA_API_KEY = '<your-api-key>'
$env:WEKNORA_BASE_URL = 'http://localhost:18080'
make eval
```

Bash：

```bash
export WEKNORA_API_KEY='<your-api-key>'
export WEKNORA_BASE_URL='http://localhost:8080'
make eval
```

如果本机没有 `make`，可直接执行相同入口：

```bash
go run ./cmd/evaluation
```

默认使用 `default` 数据集，报告写入 `tmp/evaluation-report.json`。服务可以在模型列表中选择活动模型并创建临时评测知识库，因此模型 ID 和知识库 ID 均可为空。需要固定它们时使用以下可选变量：

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `WEKNORA_BASE_URL` | `http://localhost:8080` | WeKnora 后端地址，不包含 `/api/v1` |
| `WEKNORA_TENANT_ID` | 空 | 平台级 API Key 调用指定租户时使用 |
| `EVALUATION_DATASET_ID` | `default` | 固定评测数据集 |
| `EVALUATION_KNOWLEDGE_BASE_ID` | 空 | 复用其分块、Embedding 和索引配置 |
| `EVALUATION_CHAT_MODEL_ID` | 空 | 指定回答模型 |
| `EVALUATION_RERANK_MODEL_ID` | 空 | 指定 Rerank 模型 |
| `EVALUATION_POLL_INTERVAL` | `2s` | 查询任务状态的间隔 |
| `EVALUATION_TIMEOUT` | `30m` | 单次命令的最长等待时间 |
| `EVALUATION_REPORT_PATH` | `tmp/evaluation-report.json` | 成功、失败或超时时保存的最后响应 |

`make eval` 输出 task ID、进度、Config Hash、Metric Version 和检索、答案、成本、耗时摘要。业务失败和超时会以非零状态退出，同时尽可能保留最后一次响应，便于本地验收或后续 CI 使用。

## GET `/evaluation` - 获取评估任务结果

**参数说明（查询参数）**:

| 字段     | 类型   | 必填 | 说明                                                |
| -------- | ------ | ---- | --------------------------------------------------- |
| task_id  | string | 是   | 从 `POST /evaluation` 返回的任务 ID                  |

**请求**:

```bash
curl --location 'http://localhost:8080/api/v1/evaluation?task_id=c34563ad-b09f-4858-b72e-e92beb80becb' \
--header 'X-API-Key: sk-xxxxx' \
--header 'Content-Type: application/json'
```

**响应**:

```json
{
    "data": {
        "task": {
            "id": "c34563ad-b09f-4858-b72e-e92beb80becb",
            "tenant_id": 1,
            "dataset_id": "default",
            "start_time": "2025-08-12T14:54:26.221804768+08:00",
            "status": 2,
            "total": 1,
            "finished": 1
        },
        "params": {
            "session_id": "",
            "knowledge_base_id": "2ef57434-8c8d-4442-b967-2f7fc578a2fc",
            "vector_threshold": 0.5,
            "keyword_threshold": 0.3,
            "embedding_top_k": 10,
            "vector_database": "",
            "rerank_model_id": "b30171a1-787b-426e-a293-735cd5ac16c0",
            "rerank_top_k": 5,
            "rerank_threshold": 0.7,
            "chat_model_id": "8aea788c-bb30-4898-809e-e40c14ffb48c",
            "summary_config": {
                "max_tokens": 0,
                "repeat_penalty": 1,
                "top_k": 0,
                "top_p": 0,
                "frequency_penalty": 0,
                "presence_penalty": 0,
				"prompt": "",
				"context_template": "",
				"no_match_prefix": "",
                "temperature": 0.3,
                "seed": 0,
                "max_completion_tokens": 2048
            },
            "fallback_strategy": "",
			"fallback_response": ""
        },
        "metric": {
            "retrieval_metrics": {
                "precision": 0,
                "recall": 0,
                "ndcg3": 0,
                "ndcg10": 0,
                "mrr": 0,
                "map": 0
            },
            "generation_metrics": {
                "bleu1": 0.037656734016532384,
                "bleu2": 0.04067392145167686,
                "bleu4": 0.048963321289052536,
                "rouge1": 0,
                "rouge2": 0,
                "rougel": 0
            }
        },
        "result": {
            "schema_version": "evaluation-run/v1",
            "run": {
                "run_id": "c34563ad-b09f-4858-b72e-e92beb80becb",
                "tenant_id": 1,
                "dataset_id": "default",
                "started_at": "2025-08-12T14:54:26.221804768+08:00",
                "completed_at": "2025-08-12T14:54:28.221804768+08:00",
                "status": "success"
            },
            "retrieval": {
                "precision": 0,
                "recall": 0,
                "ndcg3": 0,
                "ndcg10": 0,
                "mrr": 0,
                "map": 0
            },
            "answer": {
                "bleu1": 0.037656734016532384,
                "bleu2": 0.04067392145167686,
                "bleu4": 0.048963321289052536,
                "rouge1": 0,
                "rouge2": 0,
                "rougel": 0
            },
            "usage": {
                "status": "partial",
                "calls": {
                    "total": 3,
                    "succeeded": 3,
                    "failed": 0,
                    "items": 5
                },
                "tokens": {
                    "prompt_tokens": 100,
                    "completion_tokens": 20,
                    "total_tokens": 120,
                    "cached_tokens": 0,
                    "cache_read_tokens": 0,
                    "cache_write_tokens": 0,
                    "cache_miss_tokens": 0
                },
                "cache_status": "unreported",
                "reported_call_count": 1,
                "unavailable_call_count": 2,
                "by_model": [
                    {
                        "model_type": "chat",
                        "model_id": "chat-model-id",
                        "model_name": "chat-model",
                        "operation": "chat",
                        "usage_source": "provider_reported",
                        "calls": {
                            "total": 1,
                            "succeeded": 1,
                            "failed": 0,
                            "items": 1
                        },
                        "tokens": {
                            "prompt_tokens": 100,
                            "completion_tokens": 20,
                            "total_tokens": 120,
                            "cached_tokens": 0,
                            "cache_read_tokens": 0,
                            "cache_write_tokens": 0,
                            "cache_miss_tokens": 0
                        },
                        "duration_ms": 400
                    },
                    {
                        "model_type": "embedding",
                        "model_id": "embedding-model-id",
                        "model_name": "embedding-model",
                        "operation": "batch_embed",
                        "usage_source": "unavailable",
                        "calls": {
                            "total": 1,
                            "succeeded": 1,
                            "failed": 0,
                            "items": 3
                        },
                        "tokens": {
                            "prompt_tokens": 0,
                            "completion_tokens": 0,
                            "total_tokens": 0,
                            "cached_tokens": 0,
                            "cache_read_tokens": 0,
                            "cache_write_tokens": 0,
                            "cache_miss_tokens": 0
                        },
                        "duration_ms": 300
                    },
                    {
                        "model_type": "rerank",
                        "model_id": "rerank-model-id",
                        "model_name": "rerank-model",
                        "operation": "rerank",
                        "usage_source": "unavailable",
                        "calls": {
                            "total": 1,
                            "succeeded": 1,
                            "failed": 0,
                            "items": 1
                        },
                        "tokens": {
                            "prompt_tokens": 0,
                            "completion_tokens": 0,
                            "total_tokens": 0,
                            "cached_tokens": 0,
                            "cache_read_tokens": 0,
                            "cache_write_tokens": 0,
                            "cache_miss_tokens": 0
                        },
                        "duration_ms": 200
                    }
                ],
                "by_phase": [
                    {
                        "phase": "evaluation",
                        "usage_source": "unavailable",
                        "calls": {
                            "total": 2,
                            "succeeded": 2,
                            "failed": 0,
                            "items": 2
                        },
                        "tokens": {
                            "prompt_tokens": 100,
                            "completion_tokens": 20,
                            "total_tokens": 120,
                            "cached_tokens": 0,
                            "cache_read_tokens": 0,
                            "cache_write_tokens": 0,
                            "cache_miss_tokens": 0
                        },
                        "duration_ms": 600
                    },
                    {
                        "phase": "preparation",
                        "usage_source": "unavailable",
                        "calls": {
                            "total": 1,
                            "succeeded": 1,
                            "failed": 0,
                            "items": 3
                        },
                        "tokens": {
                            "prompt_tokens": 0,
                            "completion_tokens": 0,
                            "total_tokens": 0,
                            "cached_tokens": 0,
                            "cache_read_tokens": 0,
                            "cache_write_tokens": 0,
                            "cache_miss_tokens": 0
                        },
                        "duration_ms": 300
                    }
                ]
            },
            "cost": {
                "status": "unavailable",
                "source": "not_reported",
                "amount": null,
                "warnings": [
                    {
                        "code": "pricing_not_available",
                        "message": "Stage one does not calculate prices; token usage and call counts are reported when available."
                    }
                ]
            },
            "timing": {
                "total_wall_time_ms": 2000,
                "preparation_ms": 500,
                "evaluation_ms": 1400,
                "cleanup_ms": 100,
                "case_count": 1,
                "case_avg_ms": 1400,
                "case_min_ms": 1400,
                "case_max_ms": 1400,
                "case_p50_ms": 1400,
                "case_p95_ms": 1400,
                "model_call_cumulative_ms": 900
            },
            "cases": [
                {
                    "case_id": "1",
                    "status": "success",
                    "started_at": "2025-08-12T14:54:26.721804768+08:00",
                    "completed_at": "2025-08-12T14:54:28.121804768+08:00",
                    "duration_ms": 1400,
                    "usage": {
                        "status": "partial",
                        "calls": {
                            "total": 2,
                            "succeeded": 2,
                            "failed": 0,
                            "items": 2
                        },
                        "tokens": {
                            "prompt_tokens": 100,
                            "completion_tokens": 20,
                            "total_tokens": 120,
                            "cached_tokens": 0,
                            "cache_read_tokens": 0,
                            "cache_write_tokens": 0,
                            "cache_miss_tokens": 0
                        },
                        "cache_status": "unreported",
                        "reported_call_count": 1,
                        "unavailable_call_count": 1,
                        "by_model": [
                            {
                                "model_type": "chat",
                                "model_id": "chat-model-id",
                                "model_name": "chat-model",
                                "operation": "chat",
                                "usage_source": "provider_reported",
                                "calls": {
                                    "total": 1,
                                    "succeeded": 1,
                                    "failed": 0,
                                    "items": 1
                                },
                                "tokens": {
                                    "prompt_tokens": 100,
                                    "completion_tokens": 20,
                                    "total_tokens": 120,
                                    "cached_tokens": 0,
                                    "cache_read_tokens": 0,
                                    "cache_write_tokens": 0,
                                    "cache_miss_tokens": 0
                                },
                                "duration_ms": 400
                            },
                            {
                                "model_type": "rerank",
                                "model_id": "rerank-model-id",
                                "model_name": "rerank-model",
                                "operation": "rerank",
                                "usage_source": "unavailable",
                                "calls": {
                                    "total": 1,
                                    "succeeded": 1,
                                    "failed": 0,
                                    "items": 1
                                },
                                "tokens": {
                                    "prompt_tokens": 0,
                                    "completion_tokens": 0,
                                    "total_tokens": 0,
                                    "cached_tokens": 0,
                                    "cache_read_tokens": 0,
                                    "cache_write_tokens": 0,
                                    "cache_miss_tokens": 0
                                },
                                "duration_ms": 200
                            }
                        ],
                        "by_phase": [
                            {
                                "phase": "evaluation",
                                "usage_source": "unavailable",
                                "calls": {
                                    "total": 2,
                                    "succeeded": 2,
                                    "failed": 0,
                                    "items": 2
                                },
                                "tokens": {
                                    "prompt_tokens": 100,
                                    "completion_tokens": 20,
                                    "total_tokens": 120,
                                    "cached_tokens": 0,
                                    "cache_read_tokens": 0,
                                    "cache_write_tokens": 0,
                                    "cache_miss_tokens": 0
                                },
                                "duration_ms": 600
                            }
                        ]
                    },
                    "evidence": {
                        "qid": 1,
                        "question_fingerprint": "sha256:<question-hash>",
                        "reference_answer_fingerprint": "sha256:<reference-answer-hash>",
                        "generated_answer_fingerprint": "sha256:<generated-answer-hash>",
                        "ground_truth_pids": [1],
                        "search_pids": [2, 1],
                        "rerank_pids": [2, 1],
                        "metric_input_pids": [2, 1],
                        "unmapped_result_count": 0,
                        "metrics": {
                            "retrieval_metrics": {
                                "precision": 0.5,
                                "recall": 1,
                                "ndcg3": 0.6309297535714574,
                                "ndcg10": 0.6309297535714574,
                                "mrr": 0.5,
                                "map": 0.5
                            },
                            "generation_metrics": {
                                "bleu1": 0.037656734016532384,
                                "bleu2": 0.04067392145167686,
                                "bleu4": 0.048963321289052536,
                                "rouge1": 0,
                                "rouge2": 0,
                                "rougel": 0
                            }
                        }
                    },
                    "warnings": []
                }
            ],
            "warnings": []
        }
    },
    "success": true
}
```

以上数字仅用于说明字段结构，不是性能或质量基线。实际值必须来自本地真实评测运行。

### 四维结果语义

- `retrieval` 直接映射现有 `MetricResult.RetrievalMetrics`，没有修改 Precision、Recall、NDCG、MRR、MAP 的计算方式。
- `answer` 直接映射现有 BLEU/ROUGE 指标。它们是答案与参考答案的词面指标，不等价于 LLM-as-a-judge、事实忠实度或幻觉检测。
- `usage` 是当前 Run 外层采集到的模型调用量。Chat Token 仅在现有 `ChatResponse.Usage` 有值时标记为 `provider_reported`；Embedding 和 Rerank 暂无统一 Token Usage 时标记为 `unavailable`。
- `cost.amount` 未知时固定为 `null`，`status` 为 `unavailable`。本阶段不新增价格表，也不根据 Token 自行估算金额。
- `timing.total_wall_time_ms` 是 Run 实际墙钟耗时；`model_call_cumulative_ms` 是所有模型调用耗时相加。存在并发时两者不相等是正常的。
- `cases` 保存每个 QA Case 的归属、耗时、调用量、脱敏审计证据和失败警告，不保存 Prompt、问题、答案、文档正文或 API Key。
- `metric` 旧字段继续返回，用于兼容已有调用方；`result.retrieval` 和 `result.answer` 只是对旧指标的映射。
- `retrieval-generation/v2` 固定使用最终检索 PID 列表作为指标输入：有 Rerank 结果时使用 Rerank 排名，否则使用 Search 排名；重复 PID 只保留第一次，无法映射的结果作为不相关结果保留在分母中。
- `retrieval-generation/v1` 与 `retrieval-generation/v2` 的 corpus 和 PID 输入语义不同，历史结果不能直接做质量升降比较。

### 可重复配置与持久化

- `dataset_id` 目前只接受 `default`。服务会校验五个 Parquet 文件，并按固定顺序计算 `dataset.content_fingerprint`；文件内容变化后指纹会变化。
- `dataset.files` 是五个数据文件的 Manifest，保存文件名、SHA-256 指纹和字节数，不保存本机绝对路径。
- `config` 记录本次运行实际使用的数据集版本、模型身份、分块、检索、生成、索引、并发数和应用版本，`config_hash` 是这些有效参数的稳定 SHA-256 摘要。
- `runtime` 保存应用版本、Commit SHA、工作树修改状态和 Commit 是否可获得。构建信息不可获得时不会虚构版本，而是把可重复性标记为 `partial`。
- 模型 API Key、App ID、App Secret 和自定义 Header 不会写入评测快照；Endpoint、Prompt 和 Context 只保存指纹。扩展配置只允许非敏感行为参数进入模型参数指纹。
- `reproducibility.status` 为 `complete` 或 `partial`，表示当前定义的非敏感复现信息是否完整，并通过 `warnings` 说明数据制品、代码版本或模型配置中未能安全固化的部分。`complete` 不代表系统可以从该快照一键重放；当前没有快照重放入口，凭据和只保存指纹的配置仍需在运行环境中另行提供。
- 兼容字段 `params` 仍保留模型 ID 和数值参数，但 Prompt、Context、Fallback 和 Rewrite 文本在返回及持久化前会置空；运行中的模型调用继续使用原始配置。
- `chunking.applied=true` 表示评测语料会实际使用知识库的现有分块器。每条带 PID 的数据集 passage 独立分块，不跨 passage 边界，以保持召回结果与标准 PID 的映射。
- `task`、`config`、`metric` 和 `result` 以快照形式保存，因此源知识库或模型之后被修改、删除时，既有评测结果仍可读取。
- 主运行更新和对应 Case 结果在同一事务中写入；所有单 Run 查询均受当前 `tenant_id` 限制。
- Run 快照只保存聚合结果，不重复保存 Case 明细。读取单个 Run 时，Repository 会从 Case 表按当前租户和 Run ID 查询，再按数值 QID 稳定排序并组装回 API 响应。
- 本阶段保存每个已完成 Case 的最新进度，但不做断点续跑。多实例部署无法仅凭 `running` 状态安全判断任务所属进程，因此进程异常退出后的自动终结应在后续引入 Worker 租约或心跳后启用。

`config` 字段结构示例：

```json
{
    "schema_version": "evaluation-config/v2",
    "dataset": {
        "id": "default",
        "version": "1",
        "content_fingerprint": "sha256:<dataset-content-hash>",
        "files": [
            {
                "name": "queries.parquet",
                "fingerprint": "sha256:<queries-file-hash>",
                "size": 4096
            },
            {
                "name": "corpus.parquet",
                "fingerprint": "sha256:<corpus-file-hash>",
                "size": 65536
            },
            {
                "name": "qrels.parquet",
                "fingerprint": "sha256:<qrels-file-hash>",
                "size": 4096
            },
            {
                "name": "qas.parquet",
                "fingerprint": "sha256:<qas-file-hash>",
                "size": 4096
            },
            {
                "name": "answers.parquet",
                "fingerprint": "sha256:<answers-file-hash>",
                "size": 8192
            }
        ],
        "query_count": 100,
        "corpus_count": 1000,
        "case_count": 100,
        "ingestion_mode": "passage_chunking"
    },
    "source_knowledge_base_id": "kb-00000001",
    "models": {
        "embedding": {
            "id": "embedding-model-id",
            "name": "embedding-model-name",
            "type": "Embedding",
            "source": "remote",
            "provider": "provider-name",
            "interface_type": "openai",
            "embedding_dimension": 1024,
            "max_concurrency": 4,
            "endpoint_fingerprint": "sha256:<endpoint-hash>",
            "parameters_fingerprint": "sha256:<model-parameters-hash>",
            "updated_at": "2026-08-26T00:00:00Z"
        },
        "chat": {
            "id": "chat-model-id",
            "name": "chat-model-name",
            "type": "KnowledgeQA",
            "source": "remote",
            "provider": "provider-name",
            "interface_type": "openai",
            "max_concurrency": 4,
            "endpoint_fingerprint": "sha256:<endpoint-hash>",
            "parameters_fingerprint": "sha256:<model-parameters-hash>",
            "updated_at": "2026-08-26T00:00:00Z"
        },
        "rerank": {
            "id": "rerank-model-id",
            "name": "rerank-model-name",
            "type": "Rerank",
            "source": "remote",
            "provider": "provider-name",
            "interface_type": "openai",
            "max_concurrency": 4,
            "endpoint_fingerprint": "sha256:<endpoint-hash>",
            "parameters_fingerprint": "sha256:<model-parameters-hash>",
            "updated_at": "2026-08-26T00:00:00Z"
        }
    },
    "chunking": {
        "applied": true,
        "source_unit": "dataset_passage",
        "config": {
            "chunk_size": 512,
            "chunk_overlap": 80,
            "separators": ["\n\n", "\n"]
        }
    },
    "retrieval": {
        "vector_threshold": 0.5,
        "keyword_threshold": 0.3,
        "embedding_top_k": 10,
        "rerank_top_k": 5,
        "rerank_threshold": 0.7
    },
    "generation": {
        "max_tokens": 0,
        "max_completion_tokens": 2048,
        "temperature": 0.3,
        "top_p": 0,
        "top_k": 0,
        "seed": 0,
		"repeat_penalty": 1,
		"frequency_penalty": 0,
		"presence_penalty": 0,
        "prompt_fingerprint": "sha256:<prompt-hash>",
		"context_fingerprint": "sha256:<context-hash>",
		"no_match_prefix_fingerprint": "sha256:<no-match-hash>",
		"fallback_response_fingerprint": "sha256:<fallback-response-hash>",
		"fallback_prompt_fingerprint": ""
    },
    "indexing": {
        "vector_enabled": true,
        "keyword_enabled": true
    },
    "runtime": {
        "case_concurrency": 7,
        "metric_version": "retrieval-generation/v2",
        "result_version": "evaluation-run/v1",
        "application_version": "0.7.2",
        "commit_sha": "0123456789abcdef0123456789abcdef01234567",
        "vcs_modified": false,
        "commit_available": true
    },
    "reproducibility": {
        "status": "complete",
        "warnings": []
    },
    "config_hash": "sha256:<effective-config-hash>"
}
```

### 数据库存储职责与能力边界

| 表 | 保存内容 | 不保存内容 |
| --- | --- | --- |
| `evaluation_runs` | Run 身份、生命周期、配置快照、兼容参数快照、聚合指标和四维结果 | Case 明细、凭据、问题或答案正文 |
| `evaluation_run_cases` | Case 状态、耗时、Usage、Warning、PID 排名、指标和文本指纹 | 问题、参考答案、生成答案或文档正文 |

两张表由项目现有 PostgreSQL/SQLite 迁移创建，Case 通过外键归属 Run，删除 Run 时级联删除 Case。当前实现支持进程重启后读取已经持久化的 Run 和 Case，但不支持从中断 Case 继续执行，也不支持多实例自动接管。

SQLite 的新建库、v11 到 v12 升级、索引、外键级联和 Down Migration 已建立自动化测试。2026-08-27 已在本地 ParadeDB/PostgreSQL 17 容器中完成 migration 79 到 85 的真实升级，并验证两张表、索引、JSONB 写入、Run/Case 外键和级联删除。Down Migration 仍应在可丢弃的数据库或 CI 中验证，不能为验证回滚而破坏现有开发数据。

Repository 保留了显式的 `MarkInterruptedRunsFailed` 恢复操作，但启动流程不会自动调用。该操作目前没有 Worker 归属、租约或心跳条件，若在多实例启动时直接全局执行，可能把其他实例仍在运行的任务错误关闭。安全的异常恢复需要先增加 Worker Owner 和租约过期判断，只处理确认失去所有权的 Run；在此之前，文档和 API 不宣称支持断点续跑或多实例自动接管。

## GET `/evaluation/runs` - 分页查询历史 Run

该接口只读取当前租户的 `evaluation_runs`，再对当前页 Run ID 一次性聚合 Case 状态计数。它不会将 Run 和 Case 直接 JOIN，因此 Case 数量不会放大分页总数，也不会产生逐 Run 查询。

| 查询参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `page` | integer | `1` | 从 1 开始的页码 |
| `page_size` | integer | `20` | 每页 1 至 100 条 |
| `status` | string | 空 | `pending`、`running`、`success`、`partial` 或 `failed` |
| `dataset_id` | string | 空 | 数据集 ID 精确匹配 |
| `config_hash` | string | 空 | 配置哈希精确匹配 |
| `embedding_model_id` | string | 空 | 固化的 Embedding 模型 ID |
| `chat_model_id` | string | 空 | 固化的回答模型 ID |
| `rerank_model_id` | string | 空 | 固化的 Rerank 模型 ID |
| `started_from` | RFC3339 | 空 | 开始时间下界 |
| `started_to` | RFC3339 | 空 | 开始时间上界 |

请求示例：

```bash
curl --location 'http://localhost:8080/api/v1/evaluation/runs?page=1&page_size=20&dataset_id=default&status=success' \
--header 'X-API-Key: sk-xxxxx'
```

响应中的 `items` 是轻量 Run 摘要，包含配置身份、模型快照、进度、四维聚合结果和 Case 状态计数，但不包含完整 Case 数组：

```json
{
  "success": true,
  "data": {
    "items": [
      {
        "run_id": "run-1",
        "status": "success",
        "dataset": {
          "id": "default",
          "content_fingerprint": "sha256:<dataset-hash>"
        },
        "config_hash": "sha256:<config-hash>",
        "metric_version": "retrieval-generation/v2",
        "result_version": "evaluation-run/v1",
        "progress": {
          "total": 100,
          "finished": 100,
          "cases": {
            "total": 100,
            "pending": 0,
            "running": 0,
            "success": 100,
            "partial": 0,
            "failed": 0
          }
        },
        "retrieval": { "precision": 0.5, "recall": 0.6 },
        "answer": { "rougel": 0.4 },
        "cost": {
          "status": "unavailable",
          "source": "not_reported",
          "amount": null
        },
        "timing": { "total_wall_time_ms": 10000 }
      }
    ],
    "total": 1,
    "page": 1,
    "page_size": 20
  }
}
```

示例数字只用于说明结构，不是质量或性能基线。

## GET `/evaluation/runs/:run_id` - 获取 Run 概览

该接口返回一个 Run 摘要、完整的不可变配置快照以及兼容的旧指标字段，不加载 Case 列表：

```json
{
  "success": true,
  "data": {
    "summary": {
      "run_id": "run-1",
      "status": "success",
      "config_hash": "sha256:<config-hash>"
    },
    "config": {
      "schema_version": "evaluation-config/v2",
      "config_hash": "sha256:<config-hash>"
    },
    "metric": {
      "retrieval_metrics": { "precision": 0.5 },
      "generation_metrics": { "rougel": 0.4 }
    }
  }
}
```

Run 不存在或不属于当前租户时返回 404。历史页面展示这里保存的模型和配置快照，不重新关联模型管理页中的当前配置。

## GET `/evaluation/runs/:run_id/cases` - 分页查询 Case

| 查询参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `page` | integer | `1` | 从 1 开始的页码 |
| `page_size` | integer | `20` | 每页 1 至 100 条 |
| `status` | string | 空 | 可选 Case 状态筛选 |

```bash
curl --location 'http://localhost:8080/api/v1/evaluation/runs/run-1/cases?page=1&page_size=20&status=failed' \
--header 'X-API-Key: sk-xxxxx'
```

响应为 `{items, total, page, page_size}`。每个 Item 使用与单任务响应中 `result.cases` 相同的审计证据结构。前端只在打开详情抽屉时调用该接口；轮询任务状态时不会反复加载 Case。

## GET `/evaluation/comparison` - 比较持久化 Run

| 查询参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `baseline_id` | string | 是 | 必须出现在 `run_ids` 中的基线 Run |
| `run_ids` | string 或重复参数 | 是 | 去重后 2 至 5 个 Run ID；支持逗号分隔 |

```bash
curl --location 'http://localhost:8080/api/v1/evaluation/comparison?baseline_id=run-a&run_ids=run-a,run-b' \
--header 'X-API-Key: sk-xxxxx'
```

响应保持请求中的 Run 顺序。所有 `absolute` 和 `percent` 均为“候选值减基线值”；基线为 0 时百分比为 `null`。服务只比较已保存结果，不重新执行检索、生成或指标计算。

每个维度都有独立的 `*_compatibility`：

- 质量要求数据集内容指纹、Metric Version 和 Result Version 相同，Run 已终结，且 Retrieval/Answer 指标存在。
- 费用金额要求两侧 `amount` 非空、费用状态可比较、币种和 Pricing Version 相同。费用未知时 `amount` 及其差值保持 `null`，不会按 0 处理。
- Usage 与费用金额独立判断兼容性。Run 必须已终结且两侧 Usage 至少部分可用；部分 Usage 会附带 `usage_partial`，未报告的 Token 不会被当成完整数据。旧版 `cost` 中的 Usage 差值字段暂时保留用于客户端兼容，新客户端读取独立的 `usage` 和 `usage_compatibility`。
- Timing 在 Run 终态时返回差值，并附带 `timing_is_environment_dependent`，提醒耗时受机器负载和网络影响。
- `partial` Run 会附带警告。任何不可比较原因只影响相应维度，不会偷偷改用当前模型配置或重算旧指标。

精简响应示例：

```json
{
  "success": true,
  "data": {
    "baseline_id": "run-a",
    "runs": [
      {
        "run": { "run_id": "run-b" },
        "quality_compatibility": { "comparable": true, "reasons": [], "warnings": [] },
        "cost_compatibility": { "comparable": false, "reasons": ["cost_unavailable"], "warnings": [] },
        "usage_compatibility": { "comparable": true, "reasons": [], "warnings": [] },
        "timing_compatibility": {
          "comparable": true,
          "reasons": [],
          "warnings": ["timing_is_environment_dependent"]
        },
        "quality": {
          "precision": {
            "baseline": 0.5,
            "value": 0.6,
            "absolute": 0.1,
            "percent": 20
          }
        },
        "cost": {
          "amount": { "baseline": null, "value": null, "absolute": null, "percent": null }
        }
      }
    ]
  }
}
```

## Web 闭环与权限

Vue 页面位于 `/platform/evaluations`，提供历史筛选与服务端分页、四维摘要、Run 详情、Case 独立分页和 2 至 5 Run 基线对比。对比选择在普通翻页时保留，在重新应用筛选条件时清空，因此可以从不同页选择 Run。Viewer 可以读取历史与对比；发起评测会产生真实模型调用，按钮只对 Admin/Owner 显示，后端 RBAC 始终是最终权限来源。

页面发起评测后只轮询 `GET /evaluation?task_id=...`。任务进入成功或失败终态、页面隐藏或组件卸载时停止轮询；Case 不参与轮询。要验收持久化，应在完成至少两个 Run 后重启 App，再确认历史、详情、Case 和对比仍可读取。

## POST `/evaluation` - 创建评估任务

**参数说明（请求体）**:

| 字段              | 类型   | 必填 | 说明                                            |
| ----------------- | ------ | ---- | ----------------------------------------------- |
| dataset_id        | string | 否   | 评估数据集，留空时使用 `default`（官方测试集）   |
| knowledge_base_id | string | 否   | 源知识库 ID，留空时按活动模型使用默认配置        |
| chat_id           | string | 否   | 对话模型 ID，留空时选择活动的 KnowledgeQA 模型   |
| rerank_id         | string | 否   | 重排序模型 ID，留空时选择活动模型；不存在则跳过  |

**请求**:

```bash
curl --location 'http://localhost:8080/api/v1/evaluation' \
--header 'X-API-Key: sk-xxxxx' \
--header 'Content-Type: application/json' \
--data '{
    "dataset_id": "default",
    "knowledge_base_id": "kb-00000001",
    "chat_id": "8aea788c-bb30-4898-809e-e40c14ffb48c",
    "rerank_id": "b30171a1-787b-426e-a293-735cd5ac16c0"
}'
```

**响应**:

```json
{
    "data": {
        "task": {
            "id": "c34563ad-b09f-4858-b72e-e92beb80becb",
            "tenant_id": 1,
            "dataset_id": "default",
            "start_time": "2025-08-12T14:54:26.221804768+08:00",
            "status": 1
        },
        "params": {
            "session_id": "",
            "knowledge_base_id": "2ef57434-8c8d-4442-b967-2f7fc578a2fc",
            "vector_threshold": 0.5,
            "keyword_threshold": 0.3,
            "embedding_top_k": 10,
            "vector_database": "",
            "rerank_model_id": "b30171a1-787b-426e-a293-735cd5ac16c0",
            "rerank_top_k": 5,
            "rerank_threshold": 0.7,
            "chat_model_id": "8aea788c-bb30-4898-809e-e40c14ffb48c",
            "summary_config": {
                "max_tokens": 0,
                "repeat_penalty": 1,
                "top_k": 0,
                "top_p": 0,
                "frequency_penalty": 0,
                "presence_penalty": 0,
				"prompt": "",
				"context_template": "",
				"no_match_prefix": "",
                "temperature": 0.3,
                "seed": 0,
                "max_completion_tokens": 2048
            },
            "fallback_strategy": "",
			"fallback_response": ""
        },
        "result": {
            "schema_version": "evaluation-run/v1",
            "run": {
                "run_id": "c34563ad-b09f-4858-b72e-e92beb80becb",
                "tenant_id": 1,
                "dataset_id": "default",
                "started_at": "2025-08-12T14:54:26.221804768+08:00",
                "completed_at": null,
                "status": "pending"
            },
            "retrieval": null,
            "answer": null,
            "usage": {
                "status": "not_applicable",
                "calls": {
                    "total": 0,
                    "succeeded": 0,
                    "failed": 0,
                    "items": 0
                },
                "tokens": {
                    "prompt_tokens": 0,
                    "completion_tokens": 0,
                    "total_tokens": 0,
                    "cached_tokens": 0,
                    "cache_read_tokens": 0,
                    "cache_write_tokens": 0,
                    "cache_miss_tokens": 0
                },
                "cache_status": "not_applicable",
                "reported_call_count": 0,
                "unavailable_call_count": 0,
                "by_model": [],
                "by_phase": []
            },
            "cost": {
                "status": "not_applicable",
                "source": "not_reported",
                "amount": null,
                "warnings": []
            },
            "timing": {
                "total_wall_time_ms": 0,
                "preparation_ms": 0,
                "evaluation_ms": 0,
                "cleanup_ms": 0,
                "case_count": 0,
                "case_avg_ms": 0,
                "case_min_ms": 0,
                "case_max_ms": 0,
                "case_p50_ms": 0,
                "case_p95_ms": 0,
                "model_call_cumulative_ms": 0
            },
            "cases": [],
            "warnings": []
        }
    },
    "success": true
}
```
