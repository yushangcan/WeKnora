# 评估功能 API

[返回目录](./README.md)

| 方法 | 路径           | 描述                  |
| ---- | -------------- | --------------------- |
| GET  | `/evaluation/` | 获取评估任务结果       |
| POST | `/evaluation/` | 创建评估任务          |

> 注：服务端路由带尾斜杠（Gin 会自动从 `/evaluation` 重定向到 `/evaluation/`），下方示例为方便阅读用了 `/evaluation`。

> 阶段一说明：评测结果仍保存在服务进程内存中，服务重启后会丢失。本阶段没有新增数据库、历史查询、结果对比或 Vue 页面。

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
                "prompt": "这是用户和助手之间的对话。",
                "context_template": "你是一个专业的智能信息检索助手",
                "no_match_prefix": "<think>\n</think>\nNO_MATCH",
                "temperature": 0.3,
                "seed": 0,
                "max_completion_tokens": 2048
            },
            "fallback_strategy": "",
            "fallback_response": "抱歉，我无法回答这个问题。"
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
- `cases` 保存每个 QA Case 的归属、耗时、调用量和失败警告，不保存 Prompt、答案正文、文档正文或 API Key。
- `metric` 旧字段继续返回，用于兼容已有调用方；`result.retrieval` 和 `result.answer` 只是对旧指标的映射。

## POST `/evaluation` - 创建评估任务

**参数说明（请求体）**:

| 字段              | 类型   | 必填 | 说明                                            |
| ----------------- | ------ | ---- | ----------------------------------------------- |
| dataset_id        | string | 是   | 评估数据集，目前仅支持 `default`（官方测试集）   |
| knowledge_base_id | string | 是   | 评估使用的知识库 ID                              |
| chat_id           | string | 是   | 评估使用的对话模型 ID                            |
| rerank_id         | string | 是   | 评估使用的重排序模型 ID                          |

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
                "prompt": "这是用户和助手之间的对话。",
                "context_template": "你是一个专业的智能信息检索助手，xxx",
                "no_match_prefix": "<think>\n</think>\nNO_MATCH",
                "temperature": 0.3,
                "seed": 0,
                "max_completion_tokens": 2048
            },
            "fallback_strategy": "",
            "fallback_response": "抱歉，我无法回答这个问题。"
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
