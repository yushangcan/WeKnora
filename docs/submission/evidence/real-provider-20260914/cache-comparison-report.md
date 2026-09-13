# Luna 真实模型与 Embedding 缓存验收报告

时间：2026-09-13 至 2026-09-14（Asia/Shanghai）。三次成功运行使用 CMRC2018 固定 20 问、完整 848 段语料、Luna Chat 和本地 BGE Embedding/Rerank。以下结果来自实际 API 响应、模型 Provider 事件和数据库用量查询。

## 1. 环境与实验条件

- Chat：gpt-5.6-luna，使用用户提供的兼容接口；普通请求、流式内容及 usage 结束帧均验证成功。
- Embedding：BAAI/bge-small-zh-v1.5，512 维，本地 ONNX Runtime CPU。
- Rerank：BAAI/bge-reranker-base，本地 ONNX Runtime CPU，每问重排 30 个候选。模型文件哈希见 real-local-model-manifest.json；该文件是下载目录清单，不代表其中全部模型文件都被运行时使用。
- Case 并发：4；Temperature：0.3；max_completion_tokens：2048。三组 config_hash 相同，具体模型、分块和阈值按每组原始 .json 的 config 读取。
- Redis：独立测试实例。cold 和 warm 同用前缀 weknora:luna-acceptance:cold3:v1，TTL 24h；两组之间未重建 App 或 Redis。缓存开关属于环境设置，没有进入现有 config_hash，不能仅凭 hash 证明缓存开关一致。
- Docker 数据盘：D:/DockerData/wsl/disk/docker_data.vhdx，实测结束时 9845080064 bytes。C 盘旧路径使用目录联接指向 D 盘；模型缓存同样放在 D:/DockerData。
- 构建报告：commit_sha=5ee7afee78c277102c901e886464d6160f26d9af，vcs_modified=true，reproducibility.status=partial。原报告没有保存构建时 dirty diff，因此不声称它是某一干净提交的逐字节可复现构建；该限制保留在原始证据中。

## 2. 成功运行比较

| 组别 | 完成 | Embedding 请求/项 | Rerank 请求/项 | Chat 请求 | 准备耗时 | 总耗时 |
|---|---|---|---|---|---|---|
| disabled-20 | 20/20 | 264 / 1234 | 20 / 600 | 21 | 366241 ms | 730307 ms |
| cold-20-final | 20/20 | 264 / 1234 | 20 / 600 | 21 | 124297 ms | 718091 ms |
| warm-20 | 20/20 | 1 / 1 | 20 / 600 | 21 | 622 ms | 341215 ms |

这里的 Provider 请求按测量时间窗口统计，包含并行发生的入库等后台调用。每个 Run 实际关联的问答调用都是 20 次，另有 1 次 source=ingestion 的 Chat。warm 窗口剩余 1 次 Embedding 是没有 evaluation_run_id 的 source=chat、operation=batch_embed 后台调用；warm 的 20 个 Case 和准备阶段均未产生关联该 Run 的 Embedding Provider 用量事件。

cold/disabled 的全局数据库事件为 63 条，Run 关联事件为 61 条（20 Chat、20 Rerank、20 查询 Embedding、1 批量 Embedding）；warm 全局为 42 条，Run 关联为 40 条（20 Chat、20 Rerank）。批量装饰器记录一次逻辑调用，底层请求可能分多批发送，因此不能直接比较数据库事件数与 HTTP 请求数。

在测量窗口口径下，warm 相对 cold 的 Embedding HTTP 请求减少 99.62%（264 到 1），文本项减少 99.92%（1234 到 1）；在 Run 关联逻辑调用口径下，Embedding 从 21 次降至 0。结束后前缀匹配 1235 个 Redis 键，此数字是结束时快照，不是单独的命中率计数器。

准备阶段从 124297 ms 降至 622 ms（99.50%），总耗时从 718091 ms 降至 341215 ms（52.48%）。disabled 与 cold 以及 cold 与 warm 的本地模型预热、Rerank 排队和网络耗时不同。三组各一次，未估计置信区间；请求消除证明缓存有效，总耗时差不等于纯缓存因果效应，也不是生产环境承诺。

## 3. 质量与用量

三组所有 20 个 Case 均成功；Retrieval 全部相同：Precision=0.7854166667，Recall=MRR=MAP=NDCG@3=NDCG@10=1。

| 组别 | BLEU-4 | ROUGE-L |
|---|---|---|
| disabled-20 | 0.1413262 | 0.3469166 |
| cold-20-final | 0.1443976 | 0.3576771 |
| warm-20 | 0.1505028 | 0.3821909 |

以上答案分数描述本次输出，不能据此宣称缓存改善答案质量。Luna 温度非零且远端行为不完全可控，三组并非统计意义的多次重复实验。

| 组别 | Run 输入 Token | Run 输出 Token | Provider 缓存读取 Token |
|---|---|---|---|
| disabled-20 | 46572 | 1403 | 0 |
| cold-20-final | 46572 | 1244 | 16128 |
| warm-20 | 46572 | 1272 | 3840 |

这些 Token 来自 Run 关联的 20 次 Luna Chat，费用均为 unavailable/NULL。Embedding 与 Rerank 没有 Token 报告，Usage 为 partial；未报告不能解释为零消耗。Provider 返回的 Chat cache 字段与应用 Embedding Redis 缓存独立，不能将两者混为一种命中率。Wiki 旧/新提示词前缀对照没有在此次实验执行。

## 4. 失败尝试与证据层级

冷缓存先前尝试遇到上游 502，以及本地桥接服务重启期间连接超时。两个失败 Run 未计入三组成功结果，其任务状态摘录在 failed-attempts.json；原始失败文件仍保存在本机 tmp/luna-acceptance。恢复本地服务后使用独立 cold3 前缀重跑。

原始 API JSON、Provider 事件、数据库事件、模型文件清单均按原字节归档。此报告和 cache-comparison-summary.json 是由原始记录整理的解释材料。源码提交、真实运行的构建 SHA、后续材料 Tag 分别记录；没有把本地测试声称为新 Tag 上的远程 Actions 运行。
