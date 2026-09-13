# WeKnora 官方主线到优化完成详细设计方案

> 最终提交版请先阅读[课题 3 提交说明](submission/README.md)和[当前详细设计方案](submission/详细设计方案.md)。下文保留演进快照；最新指标版本已修正为空重排结果口径 v3。

> 本文从官方主线开始，解释当前分支这段时间为什么改、改了什么、数据如何流转、每个阶段如何验收，以及最后还缺哪些真实环境证据。
>
> 初版审计快照：`upstream/main=988cbb03`，优化分支：`codex/evaluation-four-dimension-results`，当时的实现 HEAD：`3ec3f1a9`。第 1—16 节保留该历史快照；后续实现与验收状态见第 17—18 节。本文区分已有源码和真实运行证据，没有真实 Provider、远端 Actions 或现场多实例证据的部分明确标记为“待验证”。

## 1. 先区分三类资料

这次工作同时涉及三类资料，作用不同：

1. **官方源码和官方文档**：说明 WeKnora 原本怎样运行，是实现时的事实基线。
2. **桌面上的优化方案文档**：说明目标、边界、阶段顺序和验收要求，是本次改造的设计输入。
3. **当前分支源码、测试和 Git 提交**：说明哪些设计已经实现，哪些只停留在方案或需要真实环境验证。

桌面上当前能读取到的优化方案包括：

- `优化方案/阶段一优化规划.md`：评测四维结果、Run/Case 观测和持久化的起点；
- `优化方案/阶段二优化规划-模型调用记录与模型管理统计.md`：统一模型调用事件、模型统计页面和费用语义；
- `优化方案/阶段三优化规划-Embedding结果缓存与Wiki提示词前缀优化.md`：Embedding 结果缓存和 Wiki Prompt 前缀优化；
- `优化方案/阶段三后续优化计划-缓存生产闭环与Wiki验证.md`：Redis 多实例、缓存指标、真实 Provider 验收和 CI；
- `优化方案/WeKnora评测前三项任务完整优化实施计划.md`：前三个评测任务的正确性、持久化、重启恢复边界；
- `优化方案/WeKnora前三项任务验收与审查优化计划.md`：CLI、历史 Run、质量门禁和真实验收要求。

用户最初提到的 `C:\Users\25515\Desktop\weknora需求文档.txt` 在本次审计时不在该路径，因此不能把它的内容当成已核实事实。下面的方案以当前源码、官方文档和上述可读的优化方案为依据；如果以后恢复该文件，应再做一次需求差异审查。

## 2. 官方原本主线是什么

### 2.1 官方系统组成

WeKnora 原本是一个多租户 Go 后端、Vue 前端和文档解析服务组成的 RAG 系统：

| 层 | 官方主线 | 作用 |
|---|---|---|
| Go 后端 | `internal/` | API、租户/RBAC、知识库、RAG、模型调用、异步任务和评测 |
| Vue 前端 | `frontend/` | 知识库、会话、模型设置和管理页面 |
| 解析服务 | `docreader/` | PDF、Office、网页、EPUB 等文档解析 |
| 数据库 | PostgreSQL；Lite 模式使用 SQLite | 业务数据、知识数据和迁移 |
| Redis/Asynq | Redis + Asynq Worker | 文档解析、分块、索引、摘要、问题生成、图谱、Wiki 等异步任务 |
| 检索存储 | 向量索引 + 关键词/BM25，按配置启用 | 向量检索、关键词检索和结果融合 |

本次优化不重新实现这些基础设施。原则是：保留官方 RAG 和任务主线，在外围增加可追溯观测、持久化、缓存和 CI 门禁。

### 2.2 官方文档入库主线

```text
上传文件 / URL / Markdown / FAQ
        ↓
保存文件与 Knowledge
        ↓
Redis + Asynq 异步任务
        ↓
docreader 解析
        ↓
分块：chunk size、overlap、父子分块、strategy
        ↓
Embedding
        ↓
向量索引 + 关键词索引
        ↓
摘要、问题生成、图谱、多模态、Wiki 后处理
        ↓
Knowledge/Chunk 就绪
```

### 2.3 官方文档问答主线

```text
HTTP / SSE 请求
    ↓
会话历史与查询改写
    ↓
向量检索 + 关键词检索
    ↓
RRF/结果融合
    ↓
可选 Rerank
    ↓
Web/图谱检索（按配置）
    ↓
过滤、去重、上下文组装
    ↓
Chat / ChatStream
    ↓
引用、答案与 SSE 输出
```

这条主线决定了评测的设计：评测必须调用真实 `KnowledgeQAByEvent`，不能另外写一个只测试向量库的简化评测，否则分数不能代表线上行为。

### 2.4 官方原本的评测流程

官方评测文档是 `website-docs/03-features/15-evaluation.md`。原流程是：

1. 从 `dataset/samples` 读取五个 Parquet 文件；
2. `POST /api/v1/evaluation` 创建任务；
3. 创建临时知识库并灌入 corpus；
4. 并行执行每个 QA Case；
5. 每个 Case 调用真实检索、重排和生成流水线；
6. `GET /api/v1/evaluation?task_id=...` 轮询结果。

原本已有 12 个指标：

- 检索：Precision、Recall、NDCG@3、NDCG@10、MRR、MAP；
- 生成：BLEU-1、BLEU-2、BLEU-4、ROUGE-1、ROUGE-2、ROUGE-L。

原实现的主要问题不是“没有任何评测”，而是评测缺少工程闭环：

| 原问题 | 直接影响 |
|---|---|
| 任务主要在进程内存中 | 重启后无法可靠读取历史状态 |
| 没有不可变配置快照 | 事后不知道模型、分块、检索阈值和代码版本 |
| 只有 Run 总分或有限状态 | 无法解释具体哪个 Case 退化 |
| 没有统一 Usage/Cost/Timing | 质量、调用量、耗时和成本不能关联 |
| 没有历史 Run 比较 | 无法回答“这次改动相对基线变好还是变坏” |
| 没有 CI quality gate | 回归只能靠人工发现 |
| Embedding 没有应用层结果缓存 | 相同输入重复调用 Provider |
| Wiki Prompt 稳定前缀未系统化 | Provider 前缀缓存收益不稳定 |

## 3. 总体目标和边界

最终要建立两个闭环。

### 3.1 评测闭环

```text
固定 Dataset/模型/分块/检索/生成配置
        ↓
可追溯 Evaluation Run
        ↓
Run + Case 的质量、Usage、Cost、Timing 证据
        ↓
数据库持久化
        ↓
历史 Run comparison
        ↓
CLI 与 CI quality gate
        ↓
报告 artifact 与可回退提交
```

### 3.2 成本与缓存闭环

```text
Chat / Stream / Embedding / Rerank / VLM / ASR
        ↓
统一调用事实 Wrapper
        ↓
model_usage_events
        ↓
按租户、模型、Provider、来源、时间聚合
        ↓
Token / Provider Cache / Cost 状态展示
        ↓
Embedding 应用缓存 + Wiki Prompt 前缀优化
```

### 3.3 硬边界

- 不改变官方 Retrieval、Rerank、Chat 的业务语义和既有指标公式；
- 不把 Token、调用次数或缓存 Token 当作货币金额；未知金额保持 `NULL`；
- 不保存 Prompt 正文、答案正文、文档正文、API Key、Authorization Header 或完整 Endpoint；
- 不在没有真实数据时声称缓存减少了 Provider 延迟或费用；
- 不把单元测试、Docker 测试和真实 Provider/Actions 证据混为一谈；
- 八解析引擎横评是独立可选 Phase 6，本阶段按用户要求暂缓。

## 4. 目标架构和主数据流

### 4.1 评测主链路

```text
POST /api/v1/evaluation
        ↓
Evaluation Handler
        ↓
EvaluationService
        ├─ DatasetService.LoadDataset
        ├─ NewRunConfig
        ├─ CreateRun(pending)
        ├─ 建立临时 KnowledgeBase
        ├─ 同步灌入完整 corpus，等待索引 ready
        ├─ 每个 Case 创建 run/case/phase Context
        ├─ KnowledgeQAByEvent 走真实 RAG
        ├─ Observer 记录检索、答案、Timing、Warning
        ├─ Model Usage Wrapper 记录真实调用
        ├─ SaveCaseProgress 原子保存 Run + Case
        └─ SaveTerminalRun 保存最终快照
        ↓
GET /api/v1/evaluation?task_id=...
```

### 4.2 模型调用链

```text
Provider Client
   ↓
ProviderUsage adapter（可选：Token/Amount/RequestID）
   ↓
原有重试、限流、Langfuse、Debug 逻辑
   ↓
Usage Wrapper
   ↓
Evaluation Meter / ModelUsage Recorder
   ↓
Embedding Result Cache（只对 Embedding 结果缓存）
   ↓
业务返回值
```

Embedding Cache Hit 不伪造 Provider 调用事件；批量 Embedding 的子批次不能和逻辑批量请求重复统计。

## 5. 阶段一：让单次评测结果可信

阶段一的核心不是加更多指标，而是保证已有指标的输入可靠。

### 5.1 Dataset 校验和指纹

`internal/application/service/dataset.go` 读取：

```text
queries.parquet
corpus.parquet
qrels.parquet
qas.parquet
answers.parquet
```

校验规则包括：

- ID 非负且不重复；
- qrels 引用的 query、passage 必须存在；
- qas 引用的 query、answer 必须存在；
- qrels/qas 关系不能重复；
- 每个参与评测的 query 必须有相关 passage 和参考答案；
- 稳定排序加载；
- 对文件名、大小、SHA-256、记录数生成 manifest 和整体 fingerprint。

这样相同 Dataset 才能被识别为同一输入，而不是只依赖一个字符串 ID。

### 5.2 完整 corpus 和临时知识库

评测使用完整 corpus 建临时知识库，而不是只写入每个问题的相关 passage。索引写入使用同步接口，必须等到 Knowledge 状态可检索之后再开始 Case，否则查询可能发生在索引尚未完成时，所有指标会假性偏低。

统一清理：无论评测成功、失败、取消还是持久化异常，都尝试删除临时 Knowledge 和临时 KnowledgeBase，并把清理失败作为 warning 保存。

### 5.3 passage ID 贯穿分块和检索

数据集的 PID 与 WeKnora 内部 Chunk ID 不是同一个身份。当前实现把 `evaluation_pid` 写入 chunk metadata：

```text
数据集 passage PID
        ↓
chunk metadata.evaluation_pid
        ↓
检索结果 Chunk
        ↓
还原数据集 PID
        ↓
与 qrels 比较
```

规则：

- 优先使用最终 Rerank 顺序；没有 Rerank 时使用 Search 顺序；
- 相同 PID 去重；
- 无法映射的结果使用负数占位并记录 `UnmappedResultCount`，不能静默删除；
- Case 保存 Ground Truth PID、Search PID、Rerank PID 和最终 MetricInput PID。

这保证了一个低分 Case 可以回看“返回了什么、哪些结果无法映射、最终指标使用了哪组 PID”。

### 5.4 Observer、Collector 和四维结果

`internal/evaluation/` 将一次 Run 分为：

- Run：数据集、租户、开始/完成、状态；
- Case：问题级状态、耗时、Usage、证据和 Warning；
- Phase：Preparation、Evaluation、Cleanup；
- Call：Chat、Embedding、Rerank 的真实调用记录。

`Observer` 和 `Collector` 使用锁保护，`Snapshot` 返回复制后的切片，避免并行 Case 修改数据时读到半成品。

Run 的 `EvaluationRunResult` 包含四类结果：

1. `retrieval`：Precision、Recall、NDCG、MRR、MAP；
2. `answer`：BLEU、ROUGE；
3. `usage`：调用数、Token、缓存状态、按模型/阶段聚合；
4. `cost` 和 `timing`：费用状态、阶段耗时、Case P50/P95、模型调用累计耗时。

答案指标是词面相似度，不能直接解释为事实正确、引用忠实或幻觉率；如果以后要做这些门禁，需要单独定义人工标注或 Judge 协议。

### 5.5 可复现配置

`internal/evaluation/config.go` 生成 `evaluation-config/v2` 快照，包括：

- Dataset manifest/fingerprint；
- Embedding、Chat、Rerank 模型身份、Provider、版本和维度；
- Chunk size、overlap、父子分块、分隔符和策略；
- Vector/Keyword/Rerank 阈值及 Top-K；
- MaxTokens、temperature、top-p、seed、thinking 等生成参数；
- Prompt、Context、Fallback 指纹；
- 向量/关键词开关、VectorStore；
- 应用版本、Commit SHA、工作树状态、并发度；
- metric/result version 和规范化 JSON 的 config_hash。

不保存密钥、完整 Prompt、完整 Endpoint 和正文。因此“可复现”表示输入身份可追溯，不表示不提供 Provider 凭据就能离线还原全部外部状态。

## 6. 阶段二：模型调用、Usage 和 Cost

### 6.1 统一事件模型

`model_usage_events` 一行代表一次逻辑 Provider round-trip：

| 类别 | 字段 |
|---|---|
| 身份 | call_id、tenant_id、model_id、model_name_snapshot、model_type、provider |
| 调用 | operation、source、started_at、completed_at、duration_ms、success |
| 关联 | session_id、evaluation_run_id、evaluation_case_id、trace_id |
| 用量 | prompt/completion/total token、cached/read/write/miss token、item_count |
| 状态 | usage_source、cache_status、error_message |
| 成本 | cost_amount、cost_currency、pricing_version、cost_source、cost_status |

调用口径：

- Chat 一次调用一条；
- Stream 从建立到结束最终只落一条，成功、取消、Provider 错误都要闭合；
- Batch Embedding 计一次逻辑请求，`item_count` 记录文本数；
- Rerank 计一次请求，`item_count` 记录文档数；
- 重试产生的每次真实 Provider 请求保留独立事件；
- Evaluation Meter 与全局事件表职责不同，不能在汇总层重复相加。

### 6.2 Provider Usage 为什么用 call-scoped sink

Embedding/Rerank 的公共接口原本只返回向量或排序结果，直接修改所有接口会扩大影响面。因此使用调用作用域的 `ProviderUsage`：

```text
Provider adapter 解析私有响应
        ↓
写入当前调用的 ProviderUsage sink
        ↓
Usage Wrapper 读取 sink
        ↓
ModelUsageEvent
```

当前已接入主要 Provider：

- Aliyun、Volcengine Embedding；
- Aliyun、Jina、Zhipu Rerank；
- OpenAI-compatible VLM；
- ASR 调用事实记录。

### 6.3 NULL 语义

不能把“没有上报”写成 0：

| 情况 | 存储和展示 |
|---|---|
| Provider 明确上报 0 | 保存 0 |
| Provider 没有 Token | NULL，增加未上报计数，前端显示 `—` |
| Provider 没有缓存字段 | `unreported`，命中率为 NULL |
| 不适用缓存 | `not_applicable` 或 `—` |
| 没有金额/币种 | amount=NULL，状态 unavailable/partial |
| 混合币种 | 不直接相加，按币种拆分或保持 NULL |

### 6.4 三层费用来源

```text
provider_reported
    Provider 明确返回金额、币种、口径和可选 request ID

catalog_calculated
    Provider 只返回用量，由版本化本地价格目录计算

billing_reconciled
    后续从账单/Usage API 异步对账
```

当前已实现前两层基础能力：

- `PricingResolver` 解析版本化 JSON 目录；
- Provider 明确金额优先；
- 目录计算使用 minor unit，避免浮点金额累计误差；
- 写回 `CostAmount`、`CostCurrency`、`PricingVersion`、`CostSource`、`CostStatus`；
- 价格目录通过 `WEKNORA_PRICING_CATALOG` 注入；
- 单位不完整、币种缺失或未知价格时保持 NULL，不伪造 0。

异步账单对账和真实 Provider 账单验收仍未完成。

### 6.5 API 和前端

后端提供模型用量汇总和事件明细查询，按认证上下文取得租户，不允许前端传任意 `tenant_id`。筛选维度包括时间、模型、模型类型、Provider、操作、来源和成功状态。

`frontend/src/views/settings/ModelUsagePanel.vue` 展示：

- 总调用、成功/失败；
- Token 和 Token 上报调用数；
- 缓存命中、读取、写入、未命中；
- 平均耗时；
- 按模型聚合；
- VLM、ASR 类型和调用筛选；
- 费用和费用来源；
- 分页调用明细。

## 7. 阶段三：Embedding 结果缓存

### 7.1 缓存解决什么问题

文档重建索引、重复导入或相同查询会重复调用 Embedding Provider。缓存只保存确定性的 Embedding 结果，不能缓存错误、空向量、维度错误或 NaN/Inf。

### 7.2 Cache Key

Cache Key 绑定：

```text
schema_version
tenant_id
model_id
model_updated_at/config fingerprint
provider
provider model
dimension
truncate/normalize 参数
原始 UTF-8 文本字节
```

不隐式 trim、大小写转换或 Unicode 归一化，因为这些变化可能改变 Provider 分词和向量。

### 7.3 三种运行模式

| 模式 | 实现 | 适用场景 |
|---|---|---|
| disabled | No-op，始终走 Provider | 对照实验、排障 |
| Lite | 有界 LRU + TTL + singleflight | 单机、无 Redis |
| Redis | Redis Key/Value + TTL + 分布式 token lock | 多实例部署 |

所有缓存故障必须 Fail Open：缓存读写失败时继续走原 Provider，不把缓存故障变成业务失败。

### 7.4 请求流程

```text
Get/GetMany
    ↓
命中：返回副本
未命中：按 key 进入 singleflight
    ↓
只发送 Miss 给 Provider
    ↓
校验数量、顺序、维度、有限浮点
    ↓
Set/SetMany
    ↓
恢复原请求顺序
```

Redis 多实例冷启动时：Leader 获取 token lock、调用 Provider、写缓存、释放锁；Waiter 有界退避读取缓存，超时则 Fail Open。锁必须带 owner token，不能由其他实例误释放。

### 7.5 缓存观测

应用缓存指标与 Provider Cache 分开：

- requests；
- hit_items、miss_items；
- provider_requests、provider_items；
- get/set errors；
- invalid entries；
- singleflight waits；
- lock waits。

已完成代码级测试包括 Cache Key、LRU/TTL、批量顺序、singleflight、Redis 独立 Client、多实例协调、Fail Open、模型版本失效和 cold/warm/disabled benchmark。真实 Provider 延迟、调用量和多实例故障报告仍待现场验证。

## 8. 阶段三：Wiki Prompt 前缀优化

### 8.1 优化原则

Provider 前缀缓存要求字节级前缀稳定，因此 Prompt 组织为：

```text
固定规则 + 输出 Schema + 稳定候选/共享 source context
        ↓
动态 chunks、页面正文、批变量
```

一次只修改一个模板；先做字节级前缀测试和 Fake Provider，再做真实小样本。Provider 没有返回 cache 字段时，只能声明“前缀结构稳定”，不能声明“命中率提升”。

### 8.2 当前实现和证据

`internal/agent/prompts_wiki.go` 及测试覆盖主要 Wiki 模板，包括 Citation、Page Modify、Candidate、Summary 等路径的稳定前缀、占位符和输出回归。已有脱敏 Provider cache comparison fixture，但尚无真实 Provider 命中率、费用或质量收益证据。

## 9. 阶段四：历史比较、CLI 和质量门禁

### 9.1 Comparison

历史 Run 按配置快照比较：

- Dataset fingerprint 必须兼容；
- 模型、分块、检索、生成和 metric/result version 要能解释；
- baseline 和 candidate 必须是成功 Run；
- 缺失、重复、非成功 Run ID 拒绝比较；
- 质量指标按配置容差判断；
- 成本和耗时进入报告，但当前不直接阻断质量 gate；
- 失败 Run 不能覆盖 baseline。

### 9.2 CLI

`cmd/evaluation` 提供：

1. 提交评测；
2. 轮询至终态；
3. 保存完整 JSON 报告；
4. 输出 Run ID、config hash、metric version；
5. `compare` 读取历史 Run 做比较；
6. `gate` 根据 comparison artifact 阻断质量回归。

凭据从环境读取，不写入日志和 artifact。

### 9.3 CI

已有 workflow：

- `evaluation-reproducibility.yml`：PR、定时和手动触发；
- `evaluation-migrations.yml`：PostgreSQL migration 生命周期和租约回归；
- `embedding-cache.yml`：Linux + CGO 缓存测试和 benchmark。

当前 CI 防护包括：

- 缺少服务、租户、API Key、Dataset 配置时生成明确 skipped artifact；
- PR 已配置服务但缺 baseline 时 blocked；
- 当前评测 Run 自动加入比较候选；
- 报告中的 `commit_sha` 必须等于 `GITHUB_SHA`；
- baseline/candidate 配置不兼容时不得通过 gate；
- artifact 包含 status、报告、comparison、benchmark 或 migration 证据。

migration workflow 使用 `paradedb/paradedb:v0.22.2-pg17`，因为官方全量 migration 需要 vector、pg_search 和 BM25；普通 postgres 镜像不能代表完整 schema 环境。

当前 workflow 仍依赖外部已部署的 WeKnora 服务、固定 Dataset、凭据和 baseline，尚不是“CI 自己启动 App + Redis + Stub Provider 并完成评测”的完全自包含闭环。

## 10. 阶段五：多实例 Evaluation Recovery

这是当前最后补齐的开发项之一。

### 10.1 原问题

单实例恢复可以在启动时把所有 `pending/running` Run 关闭，但多实例场景下，实例 B 启动时可能误关闭实例 A 正在运行的评测。

### 10.2 当前设计

`evaluation_runs` 增加：

- `owner_id`：每次执行生成的 UUID token；
- `lease_until`：租约截止时间；
- `heartbeat_at`：最近心跳时间；
- `(status, lease_until)` 索引。

执行规则：

1. Run 创建时同步保存 owner token 和 2 分钟租约；
2. 每 30 秒心跳续租；
3. 续租必须匹配租户、Run、owner、未终态且租约仍有效；
4. 进度、Case、终态写入都检查 owner 和有效期；
5. 心跳失去归属时取消模型调用上下文；
6. 启动和每 30 秒恢复扫描只关闭租约过期或没有租约的 Run；
7. 扫描更新时再次检查 `revision` 和租约，避免与心跳/进度更新竞争；
8. 已有进度的中断 Run 标为 `partial`，没有进度的标为 `failed`；
9. 只做中断收敛，不自动接管和重跑剩余 Case。

`owner_id` 是内部执行凭据，`json:"-"`，不出现在公共 API、配置快照或 config hash。

### 10.3 当前边界

- 断点续跑/接管尚未实现；
- 真实多实例进程故障演练尚未执行；
- 旧版本升级前应停止旧版本评测任务，再执行 migration 和部署；
- 多实例部署需要可靠时钟同步；
- migration 回退前应停止正在运行的评测。

## 11. 数据库设计总览

### 11.1 评测表

`evaluation_runs` 保存一次 Run 的身份和聚合快照：

- Run/Tenant/源 KB/临时 KB；
- Dataset version/fingerprint；
- Embedding/Chat/Rerank model ID；
- `pending/running/success/partial/failed`；
- total/finished/error；
- config、params、metric、result snapshot；
- 生命周期时间和 revision；
- owner/lease/heartbeat。

`evaluation_run_cases` 保存每个 Case 的最新结果，通过 `(run_id, case_id)` upsert，避免终态重复写入。Run Snapshot 不复制全部 Cases，读取时先读 Run，再按稳定顺序组装 Cases。

### 11.2 Model Usage 表

`model_usage_events` 按 call_id 唯一标识一次模型调用，Token、缓存 Token、金额全部保留 NULL 语义。PostgreSQL migration 当前到 `000095`，SQLite migration 当前到 `000017`。

### 11.3 租户隔离

所有 Run、Case、Model Usage 查询都强制使用认证上下文的 tenant_id；前端不能通过参数越权切换租户。

## 12. Git 提交链和每个阶段的目的

下面按功能阶段列出关键 commit。完整细粒度提交可用 `git log upstream/main..HEAD` 查看。

### 12.1 原始评测闭环

- `9e85643e fix(evaluation): persist terminal and tenant-scoped run state`：Run 终态和租户范围持久化；
- `986515fe`、`b4d22b26`、`0031eca9`：partial/failed/history 状态归一化；
- `8ebcd4c4`：终态持久化重试；
- `49c8f9e8`、`a2d02216`：前端 comparison 选择和 Usage 独立比较；
- `1a0ad70a`：评测历史页面；
- `34ca815d`、`b205def0`：本地运行和总体治理文档。

### 12.2 Model Usage

- `65fccbdd`：调用事件契约；
- `15ab50cb`：`model_usage_events` migration；
- `802fa417`：Repository；
- `d6f0d517`：durable recorder；
- `82dd0a1f`：summary/event API；
- `73093bb9`：模型统计面板；
- `f1e03d55`、`e2aa90cd`：调用和 Handler 测试；
- `7b4fa365`：Provider usage scope；
- `92929af6`：Provider request identity；
- `34b97e93`：Provider usage 和 cost 写回；
- `9c1a6441`、`b27b8ac9`：VLM/ASR；
- `1fb5fced`、`3843480c`：版本化价格目录和前端多模态展示。

### 12.3 Embedding Cache 与 Wiki

- `2088c2fe`：Embedding Result Cache 基础；
- `d0674520`：Wiki 稳定 Prompt 前缀；
- `6cc79b73`：缓存指标；
- `7bb5146e`：Redis 多实例 miss 协调；
- `db3fc1f2`：Fail Open 测试；
- `76f14b9e`：运维文档；
- `00eb0d61`、`0f034129`、`7b61bf00`：Wiki 前缀回归、重排和 Provider 对照 fixture；
- `a9b8e76f`、`8adeb9db`、`c51d7fa8`：缓存 benchmark、独立 Redis Client、模型版本失效。

### 12.4 Comparison、CI 和 migration

- `d299ce97`：baseline comparison CLI；
- `8aa255a3`、`ff9b2fce`：定时评测和质量 gate；
- `cd78022b`：PostgreSQL migration lifecycle；
- `a1603550`、`d07fcab2`：baseline 和 malformed comparison 保护；
- `0346acff`、`e97fde17`、`d1ec9d94`：PR candidate、commit SHA 和文档 guard；
- `e2685d6e`：Embedding Cache Linux workflow。

### 12.5 初版审计时最后三个实现 commit

- `3843480c`：前端模型页面补充 VLM/ASR 和费用来源；
- `a4bab041`：Evaluation owner/lease/heartbeat、过期恢复和写入 fencing；
- `3ec3f1a9`：租约回归 CI、扩展兼容 PostgreSQL 镜像和文档记录。

## 13. 验证证据如何分层

不能用“代码存在”替代全部验收。应分为以下层次：

| 层级 | 已有证据 | 仍需补充 |
|---|---|---|
| Go 单元/Repository | Dataset、Observer、Usage、Cache、Lease、Migration 测试 | 全量 race 视资源执行 |
| 前端 | `npm run type-check` 通过 | 登录后人工查看页面和真实事件 |
| Linux + CGO | Docker 中 Repository/Service/Container/Database 定向测试通过 | 远端 Actions artifact |
| PostgreSQL | ParadeDB 中全量 migration、租约 down/up 和历史行保留通过 | CI workflow 现场运行记录 |
| 真实 App | 代码和 Compose 配置已有 | 认证 POST、轮询、Run/Case 读回 |
| 真实 Provider | 适配器和 pricing 代码已有 | Token、request ID、真实费用字段和延迟 |
| 多实例 | 租约竞态测试已有 | 两个 App 进程故障/恢复演练 |
| Cache | Lite/Redis 代码级测试和 benchmark 已有 | 真实 Provider 三组延迟/调用量对照 |
| Wiki | 稳定前缀和 Fake Provider fixture 已有 | Provider 命中率和质量对照 |

## 14. 从现在到任务完成的剩余开发顺序

按用户要求，八解析引擎横评继续暂缓。后续应按以下顺序推进：

### 第一步：自包含 Evaluation CI

目标是 CI 自己启动依赖，不依赖外部手工部署：

1. 启动 ParadeDB/PostgreSQL、Redis、WeKnora App；
2. 使用固定 Dataset fingerprint；
3. 使用 Stub Chat/Embedding/Rerank Provider，避免真实凭据和随机外部网络；
4. 执行 migration；
5. 通过认证接口提交 Evaluation；
6. 轮询并确认 Run/Case 终态；
7. 保存 report、comparison、gate、数据库摘要 artifact；
8. 对固定 baseline 执行 quality gate。

### 第二步：真实环境费用和调用验收

只在安全凭据环境执行：

1. 触发 Chat、Stream、Embedding、Batch Embedding、Rerank、VLM、ASR；
2. 核对事件数与真实 Provider round-trip 数一致；
3. 核对 Token、Cache、Request ID；
4. 配置版本化 pricing catalog；
5. 核对 amount、currency、pricing_version、cost_source；
6. 没有账单依据的字段保持 NULL；
7. 报告真实费用、未上报费用和混币种边界。

### 第三步：Embedding 真实对照

固定文本、模型、维度、并发、Provider 和机器后执行：

| 组 | 缓存 | 观测 |
|---|---|---|
| Disabled | 关闭 | Provider 调用数、耗时、错误 |
| Cold | 开启但空 | 首次填充成本、缓存写入 |
| Warm | 开启且预热 | Provider 调用减少、命中数、延迟 |

至少报告 wall time、Provider requests、items、hit/miss、get/set error、降级行为；不能只报告本地 cache 函数耗时。

### 第四步：Wiki 真实小样本

固定语料和模板，按模板一次只改一个结构，记录：

- Prompt prefix fingerprint；
- Provider 是否返回 cache 字段；
- 命中/未命中或无法观测；
- 输入输出质量和耗时；
- 没有 Provider 字段时明确写“结构稳定，收益未证实”。

### 第五步：故障演练和交付

验证：

- App 重启后过期 Run 收敛；
- 另一个实例的有效租约不被关闭；
- 心跳丢失后原执行者停止写入；
- Redis 故障时 Embedding 继续走 Provider；
- Provider 失败不写入错误缓存；
- baseline 失败或配置不兼容时 gate 阻断。

## 15. 最终 Definition of Done

只有同时满足以下条件，才可以向外部汇报“任务完成”：

1. 固定 Dataset 可生成 manifest/fingerprint；
2. Run 保存配置、代码、模型和数据集身份；
3. Case 保存 PID、检索结果、答案 fingerprint、Usage、Warning 和耗时；
4. 重启和过期租约不会丢失或误关闭其他实例的 Run；
5. Model Usage 页面能展示真实调用事件，未知值显示 `—`；
6. pricing catalog 或 Provider 账单依据可解释真实金额；
7. Embedding cache 的 cold/warm/disabled 有真实 Provider 对照；
8. Wiki 结构优化有真实 Provider 证据，或明确收益未观测；
9. CI 能自包含执行 migration、评测、comparison、gate 并上传 artifact；
10. 所有重要阶段都有独立 commit，能够回退；
11. 真实 E2E、Actions、Provider、Redis 多实例证据单独归档；
12. 八解析引擎横评若要开始，另立 Phase 6，不与上述闭环混合。

## 16. 初版审计结论

当前代码已经完成了评测可观测、模型调用统计、成本解析基础、Embedding Cache、Wiki Prompt 结构优化、历史比较、质量门禁和多实例租约保护的大部分开发工作。剩余重点已经从“继续堆功能”转为“用固定环境完成真实验收”：自包含 Evaluation CI、真实 Provider 费用与调用、缓存 cold/warm/disabled 对照、Wiki Provider 证据和多实例故障演练。

换句话说，核心开发主线已经基本闭合；下一阶段应优先补证据和现场闭环，避免把代码级通过误报为生产收益。八解析引擎横评仍然是独立的可选任务。

## 17. 后续实现记录：2026-09-13 自包含 Evaluation CI

本阶段在总设计文档提交 `6c892f58` 之后实现第 14 节第一步的 CI 环境和
验收驱动。详细设计、运行命令、断言、artifact 和环境限制见
[Evaluation 自包含 CI 设计与验收](Evaluation自包含CI设计与验收.md)。

### 17.1 从外部服务依赖改为本次运行自建环境

原 workflow 需要外部 App 地址、长期 API Key 和已有 baseline；缺少配置时会
跳过。新 workflow 使用独立 `docker-compose.evaluation.yml`，从当前 checkout
编译标准 App 和 Evaluation CLI，启动临时 ParadeDB/PostgreSQL、Redis 以及
合成 Chat/Embedding/Rerank Provider。评测通过注册、登录和签发评测权限 Key
进入真实认证路径，再调用现有 Evaluation API。

固定 fixture 有两个问答 Case、两个相关段落和一个干扰段落。真实 Dataset loader
读取五个 Parquet 文件；验收脚本核对文件清单顺序、唯一性、大小和 SHA-256，
并核对 App 编译产物上报的 commit 与 checkout 一致。

### 17.2 自动验收的完整步骤

1. 创建 baseline 和 candidate 两次正常 Run，各自完成两个 Case；
2. 核对答案 fingerprint、PID、质量下限、Usage 和未知费用 NULL 语义；
3. 正常 comparison 和 quality gate 必须通过；
4. 将 Stub 切换为错误答案，执行第三次真实 Run，其 quality gate 必须拒绝；
5. 重启 App，通过历史 API 读回三次 Run 和六个 Case，并核对结果保持一致；
6. 查询数据库，检查终态和租约释放，导出 Run/Case 证据；
7. 无论成功或失败，都尝试保留日志和最终退出状态，清理本次运行的专属资源。

这补充了已有 CLI、持久化和 gate 的集成验收入口。同一 checkout 内生成的
baseline 是合成用例的契约对照，不能解释为官方主线与优化分支的真实模型质量对照。
本项也不覆盖运行中崩溃接管；完成后重启读回与租约故障演练是不同的验收目标。

### 17.3 当前验收状态与下一步

| 层次 | 2026-09-13 状态 |
|---|---|
| CI 代码、独立 Compose、固定 fixture、Stub、验收脚本 | 已实现 |
| Stub / 报告 / 数据集证据检查 | 10 项 Python 测试通过 |
| Evaluation CLI 回归 | `go test ./cmd/evaluation -count=1` 通过 |
| fixture 确定性、Compose 配置、workflow 和 Bash 语法 | 检查通过 |
| 完整入口失败语义 | Docker 不可用时退出 1，并保留失败状态和诊断文件 |
| 本次 Linux 编译及完整容器 E2E | 未完成，受本机 Docker Desktop 启动失败阻断 |
| 远端 GitHub Actions | 未执行 |

因此，第一步应标记为“实现已补齐，完整 E2E 待验收”。原第 13 节的历史
Linux/数据库定向测试结果，不代表本阶段的新集成环境已经跑通。
下一步先取得统一入口成功运行的 artifact，再按第 14 节顺序完成真实 Provider
费用与调用、Embedding disabled/cold/warm、Wiki 小样本和多实例故障演练。
八解析引擎横评继续暂缓。

## 18. 线上交付与后续修复

优化分支已推送至用户仓库 `yushangcan/WeKnora`。除了自包含 CI，还补充了
CMRC2018 固定来源接入和不依赖原始数据的离线测试；线上回归发现并修复了
Wiki Token 汇总遗漏，完善了费用来源、缓存计数和 CI 证据的验证。

GitHub Actions 已实际运行，测试结果按每项检查所使用的 commit 记录。
完整交付范围、问题原因、修复行为、运行链接与剩余真实 Provider 验收边界见
[优化闭环与线上交付记录](优化闭环与线上交付记录-2026-09-13.md)。
