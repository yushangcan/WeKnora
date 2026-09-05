# WeKnora 质量评测基线与成本可观测

> 本文用于项目理解、阶段汇报和后续实施规划。
>
> 文档基于当前工作区 `C:\Users\25515\Desktop\WeKnora` 的代码审计结果编写。当前分支为 `codex/evaluation-four-dimension-results`，HEAD 为 `e2aa90cd`；工作区存在未提交修改。凡是没有真实模型、真实数据库或真实 CI 证据的内容，均明确标注为“设计方案”“待验证”或“尚未开始”，不把设计状态表述为生产完成。

## 1. 项目概述

### 1.1 课题名称

**WeKnora——质量评测基线与成本可观测**

### 1.2 项目要解决的问题

WeKnora 已经具备较完整的 RAG 能力和评测指标实现，但此前缺少一套可以长期运行、可追溯、可比较、可进入 CI 的评测与成本观测闭环。

原始问题可以归纳为四类：

1. **评测任务状态不持久**：任务状态主要保存在进程内存中，应用重启后历史状态和运行进度可能丢失。
2. **质量回归不可感知**：检索流水线、分块参数、模型或 Prompt 发生变化时，没有稳定的基线和自动门禁，召回率下降可能直到线上才被发现。
3. **模型成本不可见**：调用用量主要出现在日志中，缺少结构化事件、模型维度聚合和费用计算；Embedding 重建索引时还会重复计算。
4. **缓存利用不足**：WeKnora 没有自有 Embedding 结果缓存；Prompt 的固定内容与可变内容没有在所有路径上形成统一的前缀缓存策略。

### 1.3 目标结果

目标不是只增加一个评测接口，而是建立如下工程闭环：

```text
固定数据集/模型/分块/检索参数
        ↓
可追溯的 Evaluation Run
        ↓
逐 Case 的检索、答案、用量、成本、耗时证据
        ↓
数据库持久化与历史对比
        ↓
基线比较与质量回归判断
        ↓
CI 定时执行、报告归档、阈值阻断
```

成本侧对应另一条闭环：

```text
Chat / Stream / Embedding / Rerank 调用
        ↓
统一 Wrapper 记录一次逻辑 Provider round-trip
        ↓
结构化 model_usage_events
        ↓
按租户、模型、Provider、操作、时间聚合
        ↓
Token / 厂商缓存 / 费用状态展示
        ↓
Embedding 自有缓存和 Prompt 前缀优化
```

## 2. 先理解官方 WeKnora 主线

### 2.1 系统组成

WeKnora 当前是一个由 Go、Vue 和 Python 服务组成的多租户 RAG 系统：

| 层次 | 主要实现 | 作用 |
| --- | --- | --- |
| Go 后端 | `internal/` | API、租户/RBAC、知识库、任务编排、RAG 问答、模型调用、评测和持久化 |
| Vue 前端 | `frontend/` | 知识库、会话、模型设置、评测历史和结果展示 |
| 文档解析服务 | `docreader/` | PDF、Office、网页、EPUB 等文档解析，输出 Markdown、图片和元数据 |
| PostgreSQL/SQLite | 数据库与迁移 | 业务数据、评测 Run/Case、模型调用事件 |
| Redis + Asynq | 异步任务基础设施 | 文档解析、分块、索引、摘要、问题生成、图谱和 Wiki 等耗时任务 |
| 向量库/关键词索引 | 按配置启用 | 向量检索、BM25/关键词检索、RRF 融合 |

### 2.2 文档入库主线

官方文档描述的主流程如下：

```text
上传文件 / URL / 手动 Markdown / FAQ
    ↓
文件存储与 Knowledge 记录
    ↓
Redis + Asynq 投递异步任务
    ↓
docreader 解析
    ↓
分块（chunk size、overlap、父子分块、strategy）
    ↓
Embedding
    ↓
向量索引 + 关键词索引
    ↓
摘要、问题生成、图谱抽取、图片多模态、Wiki 等后处理
    ↓
Knowledge/Chunk 状态变为可用
```

当前官方异步基础设施已经存在：六类 Worker Pool、DLQ/重试、Wiki 独立队列、任务巡检和按模型的分布式并发限制。因此本课题的重点不是重新实现队列，而是利用现有基础设施补齐“评测可追溯、调用可观测和回归可阻断”。

### 2.3 问答主线

一次问答大致经过以下阶段：

```text
HTTP / SSE 请求
    ↓
加载会话历史
    ↓
查询改写、意图识别和查询扩展
    ↓
向量检索 + 关键词检索（可并行）
    ↓
RRF/结果融合
    ↓
Rerank（可选）
    ↓
Web 搜索或图谱检索（按配置）
    ↓
过滤、去重、上下文组装
    ↓
Chat / ChatStream
    ↓
引用、答案和 SSE 事件输出
```

评测必须复用这条真实问答主线，不能另外写一套“只调用向量库”的简化流程，否则评测分数不能代表线上行为。

### 2.4 Wiki 主线与 Prompt 缓存切入点

Wiki 不是普通问答的一个页面，而是文档入库后的富化结果：系统从文档中抽取实体和概念、建立页面、目录与来源关联，再通过 Wiki 工具供人或 Agent 浏览。

当前代码中已经存在针对 Provider 前缀缓存的 Prompt 顺序设计：

- `WikiChunkCitationPrompt` 将稳定规则、输出 Schema、文档级候选 slug 放在变化的 `<chunks>` 之前；
- `WikiPageModifySystemPrompt` 只保留跨页面共享的系统规则；
- `WikiPageModifyUserPrompt` 将共享 source context 放在页面元数据和页面内容之前；
- `internal/agent/prompts_wiki_test.go` 已有测试，验证不同批次/页面在变化块之前的字节前缀一致。

这证明“固定内容前置、可变内容后置”的代码约束已经落地到 Wiki Prompt 路径，但还没有真实 Provider 的命中率、费用下降或耗时下降数据，因此只能称为**Prompt 结构优化已实现，收益待实测**。

## 3. 原始评测能力与痛点

### 3.1 官方已有评测实现

官方评测文档位于 `website-docs/03-features/15-evaluation.md`，原流程为：

1. 使用 `dataset/samples` 下的五个 Parquet 文件；
2. 通过 `POST /api/v1/evaluation` 创建评测；
3. 创建临时知识库并写入评测语料；
4. 逐题运行完整检索和生成流程；
5. 使用 goroutine/errgroup 并行执行 Case；
6. 通过 `GET /api/v1/evaluation?task_id=...` 轮询状态和结果。

已有指标为 12 项：

| 类别 | 指标 |
| --- | --- |
| 检索准确性 | Precision、Recall、NDCG@3、NDCG@10、MRR、MAP |
| 答案质量 | BLEU-1、BLEU-2、BLEU-4、ROUGE-1、ROUGE-2、ROUGE-L |

### 3.2 原流程的主要缺陷

| 问题 | 影响 |
| --- | --- |
| 状态在进程内存 | 重启后无法查询可靠的历史状态 |
| 没有不可变配置快照 | 事后无法确认当时使用的模型、分块和阈值 |
| 缺少逐 Case 证据 | 只有总分时，很难解释哪道题退化 |
| 没有四维结果 | 质量、用量、成本和耗时不能放在同一 Run 中分析 |
| 没有历史 Run 比较 | 不能回答“这次改动相对基线变化多少” |
| 没有质量门禁 | CI 不知道何时阻断合并 |
| 原生缓存和调用成本未结构化 | 不能按模型统计调用量和真实开销 |

## 4. 需求拆解与总体架构

### 4.1 需求到模块的映射

| 原始需求 | 主要模块 | 当前状态 |
| --- | --- | --- |
| 1. 可重复执行、结果入库 | Dataset、Run Config、Run/Case Repository、CLI | 已实现基础闭环；复现仍非一键重放 |
| 2. 检索/答案/成本/耗时四类结果 | Observer、Collector、持久化结果 Schema | 已实现代码闭环；真实模型数据待补 |
| 3. 每次调用和模型页面统计 | Usage Wrapper、`model_usage_events`、API、Vue 面板 | 调用量与缓存命中率已实现并通过代码级测试；**费用项缺 pricing 表，CostAmount 恒为 null，尚未实现**；需登录后手工验收 |
| 4. Embedding 自有缓存 | Cache Key、Redis/LRU、singleflight | 尚未开始 |
| 5. Prompt 顺序优化 | Wiki Prompt 重排与稳定前缀测试 | 已实现结构优化；收益待实测 |
| 6. CI 定时与质量门禁 | GitHub Actions、基线报告、比较脚本 | 尚未开始 |
| 7. 八解析引擎基线 | 固定语料、统一适配器、质量标注和报告 | 尚未开始 |

### 4.2 数据流设计

```text
DatasetService.LoadDataset
    ├─ 校验 queries/corpus/qrels/qas/answers
    ├─ 生成 dataset fingerprint 和文件 manifest
    └─ 返回稳定顺序的 Corpus + Cases

NewRunConfig
    ├─ 数据集身份
    ├─ Embedding/Chat/Rerank 模型快照
    ├─ 分块、检索、生成和索引配置
    ├─ Prompt/Context/Fallback 指纹
    └─ commit、工作树、版本和 config_hash

Evaluation Service
    ├─ 创建 evaluation_runs
    ├─ 创建临时知识库并确认索引就绪
    ├─ 并行执行 Case
    │   ├─ Observer 采集检索/答案证据
    │   ├─ Usage Wrapper 采集真实模型调用
    │   └─ Case upsert 写入 evaluation_run_cases
    └─ 汇总 Run 四维结果并写终态
```

## 5. 方向一：评测信息持久化与可重复执行

### 5.1 数据集契约

当前默认数据集由以下五个 Parquet 文件组成：

| 文件 | 语义 | 关键字段 |
| --- | --- | --- |
| `queries.parquet` | 查询 | `id`, `text` |
| `corpus.parquet` | 评测语料 | `id`, `text` |
| `qrels.parquet` | 查询与相关 passage 关系 | `qid`, `pid` |
| `qas.parquet` | 查询与参考答案关系 | `qid`, `aid` |
| `answers.parquet` | 参考答案 | `id`, `text` |

`internal/application/service/dataset.go` 已加入以下校验：

- 只接受 `dataset_id=default`；
- ID 不能为负数；
- query、passage、answer ID 不重复；
- qrels 不能重复；
- qrels 必须引用已知 query 和 corpus；
- qas 必须引用已知 query 和 answer；
- 每个 query 必须有相关 passage 和参考答案；
- 按稳定顺序加载数据；
- 记录每个文件的 SHA-256、大小、记录数，并生成数据集整体指纹。

### 5.2 可重复性配置快照

`internal/evaluation/config.go` 生成 `evaluation-config/v2` 配置快照，内容包括：

- 数据集 ID、版本、文件 manifest 和 fingerprint；
- Embedding、Chat、Rerank 模型 ID、Provider、接口类型、维度、并发等身份；
- Chunk size、overlap、separators、父子分块、strategy、token limit、语言；
- Vector/Keyword 阈值、Embedding Top-K、Rerank Top-K 和阈值；
- max tokens、temperature、top-p、top-k、seed、thinking 等生成参数；
- Prompt、Context、Fallback 的指纹；
- 向量/关键词索引开关和 VectorStore ID；
- 应用版本、Commit SHA、工作树是否修改、并发度、指标版本和结果版本；
- 规范化 JSON 的 SHA-256 `config_hash`。

以下敏感或不适合进入快照的内容被排除：

- API Key、App ID、App Secret；
- 自定义 Header；
- 完整 Prompt、完整 Endpoint；
- 问题、答案、文档正文。

因此当前“可重复”应准确表述为：**运行输入的非敏感身份和参数可以追溯，凭据及正文需要由运行环境重新提供**。这不是“从数据库一键还原所有环境”的完整复现。

### 5.3 Run 与 Case 持久化

当前采用两张表：

#### `evaluation_runs`

存储一次评测的完整身份和总结果：

- `run_id`、`tenant_id`、数据集身份；
- 源知识库和临时知识库 ID；
- Embedding/Chat/Rerank 模型 ID；
- `pending/running/success/partial/failed` 生命周期；
- 总 Case 数、完成数、错误信息；
- 配置、参数、指标和四维结果快照；
- 开始/完成时间、创建/更新时间、revision。

#### `evaluation_run_cases`

存储每个数据集 Case 的最新可观测结果：

- Run ID、Case ID、租户和状态；
- 开始/完成时间、耗时；
- usage、warning、result 快照；
- 检索 PID 证据、检索和生成指标；
- 失败阶段和不可映射结果警告。

Case 使用 upsert，避免终态重复写入；Run 与 Case 的更新尽量放在同一事务边界内，保证页面看到的进度、状态和结果一致。

### 5.4 检索质量输入的关键设计

评测数据集使用自己的 passage ID，而临时知识库切块后产生的是 WeKnora Chunk ID。当前通过 `evaluation_pid` 元数据把 Chunk 映射回数据集 passage ID：

```text
数据集 passage PID
    ↓ 写入临时知识库 chunk metadata
线上检索返回 Chunk
    ↓ 读取 evaluation_pid
还原数据集 PID
    ↓ 与 qrels 比较
Precision / Recall / NDCG / MRR / MAP
```

当前规则：

- 优先使用最终 Rerank 排名；没有 Rerank 时使用 Search 排名；
- 相同 PID 去重；
- 无法映射的结果不被静默丢弃，而是作为不相关占位保留在分母并记录 warning；
- Case 保存 Ground Truth PID、Search PID、Rerank PID 和最终 Metric Input PID。

这一设计保证“检索结果为何得分/不得分”可以回看，而不是只保留一个浮点数。

### 5.5 四维结果

一次 Run 同时输出四类结果。

#### A. 检索准确性

- Precision：返回结果中相关文档的比例；
- Recall：召回的相关文档占全部相关文档的比例；
- MRR：第一个相关结果排名倒数的平均值；
- NDCG@3、NDCG@10：考虑排序位置的归一化折损累计增益；
- MAP：各命中位置 Precision 的平均精度均值。

#### B. 答案质量

- BLEU-1、BLEU-2、BLEU-4：n-gram 词面重叠和 brevity penalty；
- ROUGE-1、ROUGE-2：一元/二元词重叠 F1；
- ROUGE-L：基于最长公共子序列的 F1。

这些指标反映词面相似度，**不等价于事实正确性、引用忠实度或幻觉检测**。后续如果要做事实性门禁，应增加人工标注或 LLM-as-a-judge，并单独定义评测协议。

#### C. 用量

- 调用总数、成功数、失败数、处理 item 数；
- Prompt/Completion/Total Token；
- Cached、Cache Read、Cache Write、Cache Miss Token；
- Provider 是否上报、上报调用数、未上报调用数；
- 按模型和按阶段的调用聚合。

#### D. 成本与耗时

当前耗时包括：

- Preparation、Evaluation、Cleanup 阶段耗时；
- Case 平均、最小、最大、P50、P95；
- 模型调用累计耗时；
- Run 的 `total_wall_time_ms`。

注意：并行执行时，模型调用累计耗时可以大于 Run 墙钟时间，这是正常现象。`total_wall_time_ms` 是 Run 生命周期观测，不应表述为完整 HTTP 端到端耗时。

当前成本字段已预留，但尚无生产价格表和金额计算，因此成本结果通常是：

```text
cost_status = unavailable
amount = null
```

## 6. 方向二：模型调用用量和成本可观测

### 6.1 统一调用事件

当前新增 `model_usage_events`，一条记录代表一次逻辑 Provider round-trip，而不是内部每个循环或每个 token。

覆盖的主要路径：

| 调用类型 | 记录规则 |
| --- | --- |
| Chat | 一次 Chat 调用一条事件 |
| Chat Stream | 正常关闭、Provider 错误或取消时最终写一条事件 |
| Embedding | 一次逻辑请求一条事件 |
| Batch Embedding | 整批作为一条事件，用 `item_count` 表示数量 |
| Rerank | 一次 Rerank 请求一条事件 |

事件字段包括：

- 调用 ID、租户、模型 ID 和模型名称快照；
- 模型类型、Provider、操作和来源；
- 开始/结束时间、耗时、成功状态；
- 脱敏错误类别或摘要；
- item 数量；
- 可用的 Prompt/Completion/Total Token；
- Provider 上报的缓存读取、写入、未命中信息；
- 费用金额、币种、价格版本和费用状态（当前主要是预留）；
- Session、Evaluation Run、Evaluation Case、Trace 关联。

### 6.2 空值语义

模型 Provider 可能不返回 Token、缓存或币种信息，因此不能把“未知”当成 0：

| 情况 | 存储/展示 |
| --- | --- |
| Provider 上报 Token 为 0 | 保存 0 |
| Provider 没有上报 Token | 保存 `NULL`，增加未上报计数，前端显示 `—` |
| Provider 没有缓存字段 | cache status 为 `unreported`，命中率为 `null` |
| 费用无金额或无币种 | amount 为 `null`，status 为 `unavailable`/`partial` |
| 聚合结果混合币种 | 不直接相加，隐藏金额或按币种拆分 |

这是成本可信度的关键边界：**用量可观测不等于费用已经准确计算**。

### 6.3 查询 API 与模型管理页面

当前接口：

- `GET /api/v1/models/usage/summary`：按筛选范围聚合；
- `GET /api/v1/models/usage/events`：分页查看明细。

支持的过滤维度包括：时间范围、模型 ID、模型类型、Provider、操作、来源和成功状态。租户从认证上下文获取，不接受客户端传入的 `tenant_id`，避免跨租户查询。

前端 `ModelUsagePanel.vue` 已接入模型设置页，展示：

- 总调用、成功/失败；
- Token 统计和 Token 上报调用数；
- 缓存命中率及缓存读取/写入/未命中；
- 平均耗时；
- 按模型聚合；
- 费用和费用不完整提示；
- 分页事件明细。

### 6.4 当前已验证与未验证

代码级证据：

- Usage Recorder、Wrapper、Repository、Handler、前端 API 和面板已经存在；
- Chat、Stream、Embedding、Batch Embedding、Rerank 的单元测试已覆盖；
- Linux + CGO 下相关 Go 测试通过；
- 前端 type-check、测试和构建通过；
- Docker 本地构建、服务健康检查和数据库表检查通过；
- 正确的 `WeKnora` 数据库中确认 `evaluation_runs`、`model_usage_events` 存在，迁移最终为 `92 / dirty=false`。

仍需人工或真实环境验证：

- 登录模型管理页面后触发真实 Chat、Stream、Embedding、Rerank；
- 验证失败调用和取消调用只形成一条最终事件；
- 验证真实 Provider 的 Token/缓存字段映射；
- 验证多租户隔离；
- 核对 VLM、ASR、模型调试调用是否也应纳入同一套事件。

### 6.5 厂商 API 会不会直接返回“本次调用费用”

这是成本观测设计中最容易产生误解的地方。答案不是简单的“可以”或“完全不可以”，而是要区分三种数据：

| 数据 | 是否常见于单次 API 响应 | 能否直接当作金额入账 | 当前建议 |
| --- | --- | --- | --- |
| Token、字符数、图片数、文档数等用量 | 常见，但各厂商字段不同 | 不能，它只是计费数量 | 统一解析并保存 |
| Prompt Cache read/write/miss | 只有部分厂商提供 | 不能，它只说明计费单元如何拆分 | 保存原始细分字段 |
| 本次请求的货币金额 | 较少见，通常是网关或特定 Provider 扩展字段 | 只有字段定义、币种和计费口径都明确时才可以 | 作为可选的 `provider_reported` 金额 |

大多数模型 API 的同步响应主要返回 `usage`，例如输入/输出 Token、缓存 Token 或请求处理数量；价格通常由模型、区域、服务等级、缓存状态、批处理折扣、套餐、优惠、税费和生效时间共同决定，所以厂商往往把价格放在独立的 Pricing/Billing 页面或异步用量接口，而不是放进每次推理响应。官方 OpenAI 文档也把“响应中的 usage/token 统计”和“按 token 价格计算成本”作为两个步骤，响应里的 Token 数量不是货币金额。参考：[Managing tokens](https://developers.openai.com/api/docs/guides/advanced-usage#managing-tokens) 和 [Managing costs](https://developers.openai.com/api/docs/guides/production-best-practices#text-generation)。

因此不能设计成“只要调用一次 API，所有厂商都会返回 `cost` 字段”。更可靠的方案是同时支持以下三类来源，并且保留来源信息：

1. **Provider 直接上报**：响应 Body、响应 Header 或 Provider SDK 中有明确的金额、币种、请求 ID 和计费口径。金额可以记录为 `provider_reported`，但仍需做字段和币种校验。
2. **Provider 账单对账**：Provider 的 Usage/Billing API 或账单文件在调用完成后一段时间返回聚合金额。它可能只能按账户、项目、时间窗或 request ID 对账，不一定能在同步请求结束时得到。此类数据应以 `billing_reconciled` 标记，并允许更正之前的估算值。
3. **WeKnora 本地价格表计算**：Provider 只返回用量时，WeKnora 使用带版本的价格目录计算即时成本。此类金额应标为 `catalog_calculated`，并保存计算依据，不能伪装成厂商已经返回的账单金额。

如果三种来源都不可用，事件仍然保存 Token、调用次数和耗时，`cost_amount` 保持 `null`，`cost_status=unavailable`。未知费用绝不能写成 0。

### 6.6 当前代码为什么还不能直接拿到费用

当前工作区已经为费用字段预留了结构，但调用链尚未传递货币金额：

- `internal/types/model_usage.go` 有 `CostAmount`、`CostCurrency`、`PricingVersion` 和 `CostStatus`；
- `internal/models/usage/recorder.go` 的职责是保存调用事实和 Provider 上报的 Token/缓存，不负责价格决策；
- `internal/models/usage/wrappers.go` 目前从 Chat 响应读取 `TokenUsage`，Embedding/Rerank 主要记录调用次数和 item 数量；
- `internal/models/embedding/aliyun.go`、`internal/models/embedding/volcengine.go` 等适配器虽然能解析部分 `usage.total_tokens`，但 Embedding 接口只返回向量，当前把这些 usage 丢在适配器内部；
- `internal/models/rerank/aliyun_reranker.go`、`jina_reranker.go`、`zhipu_reranker.go` 也能看到部分 usage 结构，但 `Rerank` 接口只返回排序结果，Wrapper 因而拿不到 Provider 用量；
- 全仓库没有生产价格目录、价格版本解析器或金额写入逻辑，因此当前事件的成本状态默认是 `unavailable`。

这意味着实现成本不能只在 SQL 聚合层增加一列。必须先把“Provider 原始用量/金额”从各适配器安全地传到统一 Recorder，再由独立的计价器计算或记录金额。

### 6.7 推荐的最小改造架构

#### 第一步：定义统一的 Provider 观测对象

不要把所有 Provider 私有字段直接塞进 `TokenUsage`，建议增加一个内部观测契约（名称可按项目风格调整）：

```go
type ProviderUsage struct {
    Tokens        *types.TokenUsage
    BillableUnits BillableUnits
    Amount        *ProviderMoney // Provider 明确返回的金额，可为空
    RequestID     string
    RawSource     string          // response_body / response_header / sdk
}

type ProviderMoney struct {
    Amount       string // 使用十进制定点字符串，不用 float64 做金额计算
    Currency     string // ISO 4217，例如 USD、CNY
    TaxIncluded  *bool
    Basis        string // provider 定义的计费口径
}

type BillableUnits struct {
    InputTokens       *int64
    OutputTokens      *int64
    CacheReadTokens   *int64
    CacheWriteTokens  *int64
    EmbeddingItems    *int64
    RerankDocuments   *int64
}
```

`Amount` 和 `Tokens` 都是可选的：有 Token 没有金额是正常情况；有金额但没有 Token 也可能发生在按请求数或按图片计费的接口中。

#### 第二步：用调用级 Context 传递可选观测，避免破坏现有接口

当前 `embedding.Embedder` 和 `rerank.Reranker` 接口返回类型较稳定，直接把返回值改成 `(result, usage, error)` 会影响所有 Provider、Pooler、测试桩和上层调用。建议先在 `internal/types` 或独立的低层 `internal/models/observability` 包中增加调用级 Scope：

```go
scope := types.NewProviderCallScope()
callCtx := types.WithProviderCallScope(ctx, scope)

result, err := inner.BatchEmbed(callCtx, texts)
providerUsage := scope.Snapshot()
// Wrapper 再将 providerUsage + duration + success 写入 model_usage_events
```

各 Provider 适配器解析完响应后只调用：

```go
types.ReportProviderUsage(ctx, types.ProviderUsage{
    Tokens:    normalizedTokens,
    RequestID: response.RequestID,
    RawSource: "response_body",
})
```

这样做有三个好处：

- 不改变已有 Chat、Embedding、Rerank 的业务接口；
- 每次调用有独立 Scope，多个并发请求不会把 usage 串在一起；
- 后续可逐个 Provider 增加金额解析，没有必要一次重写所有适配器。

Chat 可以继续使用 `ChatResponse.Usage`，同时把 Provider 金额放进同一调用 Scope；流式 Chat 在最终 usage chunk 或流结束时只快照一次。

#### 第三步：在适配器边界解析“明确允许的字段”

解析规则必须是 Provider-specific，而不是对所有 JSON 做模糊猜测：

| 位置 | 处理方式 |
| --- | --- |
| 正文 `usage` | 按 Provider 文档映射到统一 Token/单位结构 |
| 正文 `cost`/`price` | 只有官方契约明确说明单位、币种和含税口径时才接受 |
| Header | 只允许明确的白名单，如 request ID、已文档化的 cost header；不能把所有 Header 落库 |
| SDK 扩展字段 | 适配器显式读取，记录 SDK/Provider 版本 |
| 不认识的字段 | 忽略，不凭字段名猜测金额 |

当前项目可以按以下顺序补齐：

1. Chat 的 OpenAI-compatible、Anthropic、Ollama 路径：保留现有 Token/Prompt Cache 解析，再增加可选的 Provider cost extractor；
2. Aliyun/Volcengine Embedding：先把已有 `usage.total_tokens` 通过 Scope 传出来；
3. Aliyun/Jina/Zhipu Rerank：先传出已有 token 或 document usage；
4. 对没有 usage 的 Jina、NVIDIA、Ollama 等路径明确写 `unreported`/`unsupported`，不做假精确值；
5. 最后再为确实返回金额的 Provider 增加 `ProviderMoney` 解析。

#### 第四步：独立实现价格解析器

建议新增 `PricingResolver`，让 Wrapper 不直接写死价格：

```go
type PricingResolver interface {
    Quote(ctx context.Context, input PricingInput) (CostQuote, error)
}
```

价格键至少包含：

```text
provider
model_id 或 provider_model_name
operation（chat / embedding / rerank / vlm ...）
pricing_profile（租户/Provider 账户的价格档案，不保存凭据）
service_tier 或 region（如果影响价格）
pricing_version
effective_from / effective_to
currency
```

价格项要能表达不同计费单位：

- Chat 输入 Token、输出 Token；
- Prompt Cache read、write、uncached input Token；
- Embedding 输入 Token、文本条数或字符量；
- Rerank 文档数、查询数或 Token；
- VLM 图片数/图片 Token；
- 批处理折扣或异步任务单价。

同一个 WeKnora `model_id` 可能被不同租户或不同 Provider 账户使用，价格不能只按模型名称全局硬编码。建议价格目录使用 `pricing_profile` 区分公开价、租户合同价和 Provider 账户价；档案中只存不可逆标识或配置 ID，不存 API Key。

一个按 Token 计费的示意公式是：

```text
cost = input_billable_units  × input_price
     + output_billable_units × output_price
     + cache_read_units       × cache_read_price
     + cache_write_units      × cache_write_price
```

但是 `input_billable_units` 不能一律写成 `prompt_tokens - cache_read_tokens`。不同 Provider 对 `input_tokens` 是否已经包含缓存读写、缓存写入是否另计费的定义不同，必须由价格适配器明确给出，保留原始 counters 供审计。

### 6.8 数据库字段建议

为了兼容当前表结构，可以先采用增量字段；不要直接删除已有 `cost_amount`：

```text
provider_request_id       -- Provider request ID，用于异步账单对账
provider_cost_amount      -- Provider 明确返回的金额
provider_cost_currency
provider_cost_source      -- response_body / response_header / billing_api
calculated_cost_amount    -- WeKnora 价格表计算值
calculated_cost_currency
cost_source               -- provider_reported / catalog_calculated / billing_reconciled
cost_basis                -- JSON，记录单位数量、单价和舍入规则
cost_calculated_at
cost_reconciliation_state -- pending / matched / discrepancy
```

现有 `CostAmount` 可以在过渡期表示“当前对外展示金额”，但内部必须同时保留 Provider 原值和本地计算值，避免以后无法解释为什么金额发生变化。更长期应把金额从 `DOUBLE PRECISION/float64` 迁移为以下任一安全形式：

- `NUMERIC(20, 10)` + 十进制定点库；或
- `amount_minor`/`amount_micros` 整数 + ISO 币种。

不要使用二进制浮点直接累计账单金额。迁移时要同时兼容 PostgreSQL 和 SQLite，并测试旧事件读取。

### 6.9 事件写入时的决策顺序

推荐在 `DatabaseRecorder.Record` 前完成一次不可变的 Cost Quote：

```text
ProviderUsage.Amount 存在且契约校验通过
    → cost_source=provider_reported
    → 保存 Provider 金额、币种、request_id

否则价格目录匹配且所需单位齐全
    → cost_source=catalog_calculated
    → 保存计算金额、pricing_version、cost_basis

否则已有账单对账任务尚未返回
    → cost_status=unavailable 或 pending
    → 保留 Token/单位，后续异步回填

本地模型明确不产生 Provider 账单
    → cost_status=not_applicable

任意部分调用/部分币种/部分单位缺失
    → cost_status=partial，金额只在可安全解释时展示
```

Provider 金额和本地计算金额不一致时不要静默覆盖，建议：

- 以 Provider 明确账单为对账优先值；
- 保留两者和差额；
- 超过配置误差阈值产生 `cost_discrepancy` 告警；
- 价格目录变更不会改写历史事件，只影响新调用和显式重算任务。

如果需要把美元、人民币等不同币种换算成统一报表币种，必须额外保存汇率来源、汇率版本和生效时间；在没有可靠汇率时，按币种分组展示，不直接相加。

### 6.10 重试、流式和批量请求的费用口径

成本统计还必须明确“逻辑调用”和“Provider 实际尝试”的区别：

1. **重试**：一次业务请求可能产生多次 Provider round-trip，某些 Provider 即使返回错误也可能计费。事件应增加 `attempt_count`，必要时增加子表 `model_usage_attempts`，不能只保存最后一次成功响应。
2. **流式**：费用通常在最终 chunk 才知道；正常结束、Provider 错误、客户端取消都要落一条最终事件。若只拿到部分 usage，要标 `partial`，不能把缺失部分当 0。
3. **Batch Embedding**：`item_count` 是业务输入数量；如果底层 Pool 拆成多个请求，应同时记录逻辑调用和实际尝试数，避免既重复计数又漏计费。
4. **Rerank**：有的 Provider 按文档数收费，有的按 Token 收费。只有拿到对应计费单位才能计算精确成本；仅有 `len(documents)` 时只能在价格契约明确按文档收费的情况下计算。
5. **本地 Ollama/自托管模型**：没有厂商云账单不代表业务成本为 0。可以标记 `not_applicable`，以后另建 GPU、机器、能耗或租赁成本模型，不和云 API 费用混为一谈。

### 6.11 这一方向的测试矩阵

实现后至少需要覆盖：

| 场景 | 预期 |
| --- | --- |
| Provider 返回 Token，无 cost | 保存 Token，走价格目录或 `unavailable` |
| Provider 返回 cost + currency | 校验后保存 `provider_reported` |
| cost 有金额无币种 | 不展示金额，`partial/unavailable` |
| 不同币种聚合 | 不直接相加，按币种拆分或隐藏总额 |
| Provider cost 与目录计算不一致 | 两者并存，产生差异告警 |
| Chat Stream 最终 chunk 带 usage | 只写一条完整事件 |
| Stream 中途取消 | 写一条失败/partial 事件，保留已有 usage |
| Embedding/Rerank 并发调用 | Scope 隔离，不串 usage 或 cost |
| Provider 重试 | attempt 数和实际计费不漏记 |
| 价格版本切换 | 新事件使用新版本，旧事件不被改写 |
| Redis/数据库观测写入失败 | 主模型调用不失败，但有观测告警 |
| 租户隔离 | 一个租户无法查询另一个租户的事件和成本 |

### 6.12 对当前项目最实际的落地顺序

不建议一开始就为所有厂商实现“自动读账单”。当前最稳妥的顺序是：

1. **先补齐用量传递**：把 Aliyun/Volcengine Embedding、Aliyun/Jina/Zhipu Rerank 已解析的 usage 送入统一事件；
2. **增加 `cost_source` 和 Provider request ID**：先把来源和对账链路建起来；
3. **实现一版版本化价格目录**：优先覆盖实际使用最多的 Chat Provider，再覆盖 Embedding 和 Rerank；
4. **用 Decimal/整数金额写入**：兼容旧 `cost_amount`，新增字段逐步迁移；
5. **接入真实 Provider 的可选 cost 字段**：只有文档明确的字段才标 `provider_reported`；
6. **增加异步对账任务**：对支持账单 API 的 Provider 通过 request ID 或时间窗补齐/校正；
7. **最后再做前端展示和 CI 成本门禁**：只有 `available` 或可解释的 `partial` 成本才参与预算判断。

换句话说，**现在应该实现的是“Provider 用量/金额可选上报 + WeKnora 价格目录兜底 + 来源和版本可审计”三层结构，而不是等待所有厂商在一次 API 响应里返回统一费用字段。**

> **代码级已确认缺口**：`GetVLMModel`（`model.go:611`）和 `GetASRModel`（`model.go:651`）当前直接返回底层实例，没有 `WrapChat`/`WrapEvaluationMeter` 等价包装，因此 VLM 和 ASR 的业务调用不会产生 `model_usage_events` 记录。前端筛选 `model_type=VLM/ASR` 时结果恒为空。这不是"待验证"，而是已知未接入，需后续补 Wrap。

## 7. 方向三：Embedding 自有缓存设计

### 7.1 当前状态

当前没有发现真正的 Embedding 结果缓存：

- 没有 `EmbeddingCache`；
- 没有以“文本 + 模型”为 Key 的 Redis/LRU/数据库向量缓存；
- 没有 embedding hit/miss 统计；
- 没有失效策略。

查询向量分组、Batch Embedding、Pool Embedding 只能减少一次流程内的重复或并发开销，不等于跨请求、跨重建任务复用结果。

### 7.2 建议架构

```text
规范化文本
  + Embedding 模型 ID
  + 模型配置版本
  + 向量维度
        ↓ SHA-256
缓存 Key
        ↓
Redis（跨实例）+ 本地 LRU（热点）
        ↓ miss 时 singleflight 合并并发请求
Embedding Provider
        ↓ 成功结果写回缓存
```

建议 Key 至少包含：

```text
embedding:v1:{model_id}:{model_config_hash}:{dimension}:{text_sha256}
```

### 7.3 必须先确定的边界

1. **跨租户共享**：相同模型和文本是否允许跨租户复用；默认应只共享不敏感的向量结果，或者按租户隔离，避免隐私和删除语义不清。
2. **缓存原文**：原则上不存原文，只存哈希和向量；日志也不打印原文。
3. **容量与 TTL**：Redis 最大容量、TTL、淘汰策略和热点保护需要配置化。
4. **并发击穿**：使用 singleflight 或分布式锁，避免同一文本同时 miss 时重复调用 Provider。
5. **失败缓存**：Provider 失败结果不缓存，防止暂时性故障被放大。
6. **模型变更**：模型 ID、Provider 配置、维度变化时必须自动失效或生成新 Key。
7. **向量库兼容**：Embedding 模型/维度变化通常需要重建已有知识库向量，不能只切换缓存 Key。
8. **观测口径**：缓存命中不应伪造 Provider 调用；需要区分 `cache_hit`、`provider_call` 和 `cache_lookup`，并明确是否为命中生成 usage event。

### 7.4 验收标准

- 同文本、同模型、同配置的第二次请求不产生 Provider Embedding 调用；
- 不同模型、不同维度、不同配置不能误命中；
- 并发 100 个相同请求只产生一个 Provider round-trip；
- Redis 不可用时有明确降级策略，不阻断原有索引流程；
- 命中率、miss 数、击穿次数和 Provider 调用数可查询；
- 删除/重建知识库时不泄漏原文或跨租户数据。

## 8. 方向四：Prompt 拼装顺序与厂商原生缓存

### 8.1 原则

多数厂商的 Prompt Cache 依赖请求前缀的字节稳定性。应将重复率高、跨请求不变的内容放在前面，把本轮问题、文档块、页面变量等变化内容放在后面。

目标顺序：

```text
固定系统规则
  → 输出格式/工具 Schema
  → 文档级稳定上下文
  → 页面/候选身份
  → 本批次 chunks、当前问题、临时变量
```

### 8.2 当前已完成内容

Wiki 相关 Prompt 已完成两类优化：

1. `WikiChunkCitationPrompt`：规则、JSON Schema、candidate slugs 在 `<chunks>` 之前；
2. `WikiPageModifyPrompt`：共享 source context 在页面元数据、现有页面和新内容之前。

配套测试验证：

- 不同批次的 `<chunks>` 变化不影响之前的共同前缀；
- 必要占位符仍然存在；
- 页面变量不提前打断共享 source context；
- 内部 chunk handle 不会泄露到最终页面。

### 8.3 未完成内容

- 尚未接入真实 Provider 统计命中率；
- 尚无改造前/改造后的 Token 费用对照；
- 尚无 Provider 维度的延迟对照；
- 尚未证明所有 Chat、Agent、VLM、ASR 路径都遵循同一顺序。

因此汇报时应说：**已完成 Prompt 前缀稳定性改造和回归测试，实际命中率与成本收益需要在配置了真实 Provider 的环境中测量**。

## 9. 方向五：CI 定时评测与质量回归门禁

### 9.1 当前状态

当前 `.github/workflows` 已有 App、Frontend、Docreader、CLI、Lint 等工作流，但尚未发现：

- `make eval` 或 `cmd/evaluation` 的 CI 调用；
- 评测专用定时 Workflow；
- 基线 JSON/HTML 报告；
- Recall/Precision 阈值比较；
- 合并阻断逻辑；
- 真实模型凭据和评测服务环境。

- `make eval` / `go run ./cmd/evaluation` 已提供本地自动化入口：创建评测、轮询状态、保存最后一次 JSON 响应，并在业务失败或超时时返回非零状态。但该入口的模型、分块参数由环境变量传入并经服务端 `config.Conversation` 解析，不是命令行固定；也没有读取历史 Run 的 `config_snapshot` 一键重放能力。这是 CI 接入基础，但不是 CI 门禁本身。

### 9.2 建议的 CI 分层

#### PR 快速门

目标是快速发现代码契约错误，不依赖外部模型：

1. 数据集结构和 fingerprint 审计；
2. 指标函数单元测试；
3. 配置 hash 稳定性测试；
4. Run/Case Repository 测试；
5. Usage Wrapper 测试；
6. 前端类型检查和构建；
7. 不涉及真实质量分数的静态检查。

#### 定时完整评测

建议每天或每周执行：

1. 启动固定版本的评测服务；
2. 进行模型连接预检；
3. 固定数据集、模型、分块和检索参数；
4. 执行完整 `make eval`；
5. 归档 Run JSON、配置 hash、环境信息和日志摘要；
6. 与指定基线 Run 比较；
7. 输出 Markdown/HTML 摘要和趋势数据。

#### 合并阻断门

建议只对“评测成功且数据完整”的 Run 进行质量比较：

```text
评测服务启动失败       → infrastructure failure
模型连接失败           → provider unavailable
Run 业务失败/超时       → evaluation failure
Token/成本缺失          → observability partial
评测成功但指标下降     → quality regression
```

前四类不能被转换成质量分数 0，否则会把基础设施故障误判为模型质量退化。

### 9.3 阈值策略

阈值不应只写成一个“总分”，建议按指标分层：

| 层级 | 示例策略 |
| --- | --- |
| 硬门禁 | Recall@K 不得下降超过绝对阈值；关键业务 Precision 不得下降 |
| 软门禁 | NDCG、MRR、MAP 允许小幅波动，但连续多次下降需告警 |
| 答案观察 | BLEU/ROUGE 只作趋势，不单独代表事实正确性 |
| 性能门禁 | P95 Case 耗时和模型调用累计耗时不能超过预算 |
| 成本门禁 | 已知同币种费用超过预算才阻断；未知费用不当作 0 |

阈值文件应随代码版本化，例如：

```json
{
  "baseline_run_id": "...",
  "config_hash": "sha256:...",
  "gates": {
    "recall": {"max_absolute_drop": 0.02},
    "precision": {"max_absolute_drop": 0.02},
    "ndcg10": {"max_relative_drop": 0.05},
    "case_p95_ms": {"max_relative_increase": 0.20}
  }
}
```

具体阈值必须根据多次稳定 Run、模型波动和业务容忍度确定，不能凭一次运行臆造。

## 10. 方向六：八个解析引擎横向基线（选做）

### 10.1 当前状态

项目已有解析器注册表和多种解析能力。`internal/infrastructure/docparser/engines.go` 注册了 8 个引擎常量：`builtin`（DocReader Python 解析套件）、`simple`（Go 原生文本/图片处理）、`anydoc`（进程内 Office 文档转换）、`weknoracloud`（托管解析服务）、`mineru`（自托管 MinerU）、`mineru_cloud`（MinerU 云 API）、`paddleocr_vl`（自托管 PaddleOCR-VL）、`paddleocr_vl_cloud`（PaddleOCR-VL AI Studio 云 API）。但没有发现"八个引擎统一输入、统一输出、统一评分"的完整基准框架。因此该方向当前属于**尚未开始**，不能把已有解析器注册表称为八引擎质量基线。

### 10.2 统一实验协议

要保证横向比较有效，必须固定：

- 同一批原始文档和文件哈希；
- 同一文件版本和页序；
- 同一输出格式，例如 Markdown + metadata + images；
- 同一清洗、去噪和分块规则；
- 同一执行环境和超时；
- 每个引擎的版本、配置和可用性；
- 失败、超时、未安装、模型不可用单独记录为 `unavailable`，不能填 0 分。

### 10.3 建议指标

| 维度 | 指标示例 |
| --- | --- |
| 文本完整性 | 文本覆盖率、段落/字符召回率 |
| 结构 | 标题层级保留率、列表顺序、页序正确率 |
| 表格 | 表格识别率、行列结构保留率、单元格错误率 |
| 图片 | 图片引用保留率、caption 对齐率 |
| OCR | 字符错误率、数字错误率、专有名词错误率 |
| 语义 | 人工标注的内容完整性、事实遗漏率 |
| 下游 | 在固定 RAG 配置下的 Recall/NDCG/答案质量 |
| 工程 | 成功率、平均/P95 解析耗时、内存和费用 |

建议将解析质量与下游 RAG 效果分开报告：解析文本指标回答“解析得对不对”，RAG 指标回答“解析结果是否有利于检索和回答”。

## 11. 已完成进度总览

### 11.1 已实现并有代码/测试证据

- 默认 Parquet 数据集校验、稳定加载和 fingerprint；
- `evaluation-config/v2` 非敏感配置快照和 `config_hash`；
- `evaluation_runs`、`evaluation_run_cases` 持久化模型；
- 历史 Run 分页、详情、Case 查询和 2～5 个 Run 对比 API；
- 四维结果 Schema：检索、答案、用量、成本/耗时；
- `evaluation_pid` 检索证据回映射和不可映射结果警告；
- `model_usage_events` 表、Repository、Recorder、Wrapper、查询 API；
- Chat、Stream、Embedding、Batch Embedding、Rerank 的调用记录；VLM/ASR 尚未接入统一 Wrapper；
- 模型设置页 `ModelUsagePanel.vue`；
- Wiki Prompt 稳定前缀重排及测试；
- `make eval` / `cmd/evaluation` 本地自动化入口（参数由环境变量驱动，模型/分块由服务端解析，非一键固定复现）；
- Linux + CGO 相关 Go 测试、前端检查/构建、Docker 本地构建和健康检查。

### 11.2 已实现但缺少真实端到端验证

- 真实 Provider 调用产生的 Token、缓存字段是否全部正确映射；
- 登录后的模型管理页面展示、分页和租户隔离；
- 真实评测 Run 从创建临时知识库到完成四维结果；
- Prompt 重排在真实厂商上的 cache hit、Token 和费用变化；
- 多次真实 Run 的基线波动范围；
- VLM、ASR 和模型调试调用是否纳入统一事件（当前 VLM/ASR 是已确认的代码缺口，不是已完成能力）。

### 11.3 设计具备但生产闭环未完成

- 成本字段和费用状态已预留，但没有价格表、价格版本和金额计算；
- 启动恢复已能收口遗留 `pending/running` Run，但不是断点续跑或多实例接管；
- CI 所需的本地 CLI 和 JSON 报告基础已具备，但没有 Workflow、基线和门禁；
- 评测配置可追溯，但凭据和正文不在快照中，尚不是一键重放。

### 11.4 尚未开始

- Embedding 自有缓存；
- 缓存击穿、失效和跨租户策略；
- 真实价格计算与币种安全聚合；
- CI 定时评测和合并阻断；
- 八解析引擎统一质量基线。

## 12. 难点、卡点与风险

### 12.1 Git 与主线偏差

当前分支相对 `upstream/main` 落后 76 个提交、领先 46 个提交。评测和模型用量改动涉及迁移、后端、前端和文档，不能直接假设与官方主线无冲突。后续合并前必须：

1. 先建立可回退提交或补丁存档；
2. 记录当前工作区未提交改动；
3. 在独立临时数据库测试全新建库；
4. 测试旧库升级到新版本；
5. 测试 Down Migration；
6. 检查上游是否已占用迁移编号。

### 12.2 迁移编号冲突

当前工作区曾使用过 `000090/000091`，为避开官方主线已有版本，现调整为 PostgreSQL `000091_evaluation_runs`、`000092_model_usage_events`，SQLite 对应 `000013/000014`。这些调整仍需在合并主线后重新验证，不能仅凭文件名认定迁移安全。

### 12.3 Windows CGO 阻塞

Windows 原生全量 Go 测试可能在根模块构建阶段出现：

```text
undefined: pg_query.Parse
undefined: pg_query.Deparse
```

该现象更像缺少 GCC/CGO 环境，而不一定是本次业务代码错误。应使用 Linux + CGO Docker builder 或 CI 验证受影响包，同时在汇报中明确“Windows 本机全量测试受环境阻塞”。

### 12.4 评测恢复边界

当前启动恢复逻辑：

- 查找 `pending/running` Run；
- 无已完成 Case 的 Run 标记为 `failed`；
- 有进度的 Run 标记为 `partial`；
- 写入完成时间、错误信息、revision 和 result snapshot；
- 条件更新保证重复执行基本幂等。

它不具备：

- 未完成 Case 重新执行；
- Worker owner；
- 租约和心跳；
- 多实例安全接管。

如果多个应用实例同时启动，可能把另一实例仍在执行的 Run 误收口。因此当前只能称为**单实例异常退出后的安全收口尝试**。

### 12.5 观测写入失败与性能策略

Usage 记录是观察性的：数据库写入失败不应让原始 Chat/Embedding/Rerank 调用失败，但会造成观测缺口。需要后续增加：

- 写入失败计数和告警；
- 重试或本地短暂缓冲；
- 观测数据丢失率；
- 不影响主链路的降级说明。

此外，当前 `DatabaseRecorder.Record`（`recorder.go:67`）在请求路径内同步执行 `db.Create`，没有 channel/批量 flush 缓冲。一次完整 RAG（embed + rerank + chat）额外产生 3 次 INSERT，每条事件维护 5 个二级索引。在高并发问答场景下，同步单行写入会放大数据库延迟并占用请求线程。后续应改为异步缓冲 + 批量落库，或至少在写入前做轻量采样/聚合。

### 12.6 评测成本与外部服务依赖

真实模型评测会产生 Provider 费用，并受 API Key、网络、限流、模型版本和服务可用性影响。CI 设计必须区分：

- 代码测试；
- 模型连接预检；
- 真实质量评测；
- Provider 故障；
- 质量回归。

### 12.7 数据隐私和安全

评测和用量记录不得保存：

- API Key、Bearer Token、App Secret；
- 完整 Endpoint 和自定义 Header；
- 原始问题、答案、Prompt、Response 和文档正文（除非另有合规审批）。

错误信息也必须脱敏，避免把 Provider 返回的请求片段或凭据写入数据库。

## 13. 边界与约束

### 13.1 数据边界

- 默认只支持 `dataset_id=default`；
- 数据集文件变更必须导致 fingerprint 变化；
- qrels、qas 和 corpus 关系必须完整；
- 未映射检索结果不能静默删除；
- 缺失 Token/缓存/费用不能当作 0。

### 13.2 运行边界

- 评测复用真实 RAG 流程，不另造简化链路；
- Case 可并行，但写入必须幂等；
- Run 墙钟耗时与模型累计耗时分开解释；
- 评测恢复当前不等同于断点续跑；
- 真实 Provider 不可用时不能生成质量 0。

### 13.3 兼容边界

- PostgreSQL 和 SQLite 迁移必须分别测试；
- 与官方主线合并时重新确认迁移编号；
- API 保留旧 `metric` 字段兼容，新增字段不能破坏既有客户端；
- 前端按空值语义展示 `—`；
- 模型删除或重命名后，历史事件仍使用名称快照保证可读。

### 13.4 实验纪律

每次真实实验前必须：

1. 确认当前 Git 分支和 HEAD；
2. 确认工作区是否干净；
3. 建立可回退提交或保存补丁；
4. 固定数据集、模型、分块和检索参数；
5. 一次只改变一个主要变量；
6. 保存配置 hash、数据集 fingerprint、环境、时间、并发度和报告；
7. 记录失败原因，不把失败转换为 0 分。

## 14. 分阶段实施计划

### 阶段 0：主线同步与基线冻结

**目标**：在继续开发前建立可信起点。

工作项：

- 处理当前分支与 `upstream/main` 的迁移和代码冲突；
- 完成 PostgreSQL/SQLite 全新建库、升级和回滚测试；
- 固定默认数据集 fingerprint；
- 建立基线模型和配置清单；
- 明确真实 Provider 测试预算和凭据注入方式。

验收：

- 新旧数据库迁移均成功；
- `schema_migrations` 无 dirty 状态；
- 基线 Run 有完整 config hash 和数据集 fingerprint；
- 代码、报告和环境信息可回退。

### 阶段 1：评测持久化稳定化

**目标**：让 Run、Case、历史和四维结果成为稳定契约。

工作项：

- 完成 Run/Case 事务一致性测试；
- 验证重启收口和终态规范化；
- 完成历史分页、详情、对比页面验收；
- 增加评测结果 Schema 兼容测试；
- 明确单实例恢复文档边界。

验收：

- 服务重启后可查询历史 Run；
- 每个 Case 能回看 PID 证据、指标、用量和 warning；
- partial/failed/success 语义一致；
- 多租户无法互查。

### 阶段 2：真实模型调用观测

**目标**：完成从真实 Provider round-trip 到模型页面的闭环。

工作项：

- 登录后触发各类真实调用；
- 检查 Stream 正常结束、错误和取消只写一条事件；
- 检查 Batch Embedding 的 item_count 和调用次数；
- 核对厂商 Token 和缓存字段；
- 确认 VLM、ASR 和调试调用范围。

验收：

- 每种操作都能在事件页找到对应记录；
- 未上报数据显示 `—`；
- 租户隔离通过；
- 事件写入失败不影响主调用，且有可观测告警。

### 阶段 3：费用计算

**目标**：从“用量可见”升级为“费用可解释”。

建议新增价格表：

```text
Provider + Model + Operation + Token Type + Pricing Version + Effective Time
```

工作项：

- 输入/输出 Token 分价；
- 缓存读取/写入 Token 分价；
- Embedding 按 Token 或文本量计价；
- Rerank 按文档数或 Token 计价；
- 价格版本和生效时间；
- 币种拆分聚合；
- 缺价格时保留用量、费用标记 unavailable/partial。

验收：

- 测试价格表可以重算已知样例；
- 混合币种不错误求和；
- 价格变更不改写历史事件；
- 页面能区分 available、partial、unavailable。

### 阶段 4：Embedding 缓存

**目标**：减少重复索引和重复问答中的 Embedding Provider 调用。

工作项：

- 实现规范化文本和配置版本 Key；
- Redis + 本地 LRU 两级缓存；
- singleflight 防击穿；
- 成功写缓存，失败不缓存；
- TTL、容量和失效策略；
- hit/miss/Provider call 指标；
- 模型维度变化触发重建提示。

验收：

- 相同输入跨请求命中；
- 并发击穿测试通过；
- 模型切换不误命中；
- Redis 故障可降级；
- 命中率能出现在评测和模型页面。

### 阶段 5：CI 回归门禁

**目标**：把稳定评测接入研发流程。

工作项：

- 新增评测 Workflow 和定时触发；
- 配置固定 Runner、模型和数据集；
- 预检、正式评测、报告归档；
- 基线选择和阈值比较；
- 区分 infrastructure/provider/evaluation/quality failure；
- PR 门禁与定时完整评测分层。

验收：

- 定时任务能产生可下载报告；
- 真实退化超过阈值时阻断；
- Provider 不可用时任务标记 unavailable 而非质量 0；
- 同一 config hash 下报告可比较。

### 阶段 6：解析引擎基线（选做）

**目标**：在同一批文档上比较八个解析引擎。

工作项：

- 冻结文档集和 hash；
- 为八个引擎实现统一适配器；
- 统一输出和清洗规则；
- 记录版本、耗时、内存、失败和不可用；
- 建立人工标注子集；
- 输出解析质量和下游 RAG 双层报告。

验收：

- 每个引擎都能追溯输入、版本和配置；
- unavailable 不填 0；
- 文本、结构、表格、图片、OCR 和下游指标均有定义；
- 报告能解释引擎选择，而不仅是给出一个总分。

## 15. 汇报时可以直接使用的项目总结

### 15.1 一分钟版本

本项目围绕 WeKnora 的 RAG 质量和模型成本建立可追溯闭环。我们先把评测数据集、模型、分块、检索和生成参数固化为带 fingerprint 和 config hash 的配置快照，再把一次评测拆成 Run 和 Case 持久化，使服务重启后仍能查询历史，并同时输出检索质量、答案质量、模型用量和耗时。成本侧新增统一模型调用事件，覆盖 Chat、流式 Chat、Embedding、Batch Embedding 和 Rerank，在模型管理页面展示调用次数、Token、厂商缓存和耗时。Wiki Prompt 已完成固定内容前置、可变内容后置的稳定前缀优化。当前 Embedding 自有缓存、真实价格计算、CI 质量门禁和八解析引擎基线仍是后续工作；因此现阶段准确的结论是“用量和评测可观测基础已完成，费用和自动回归闭环正在建设”。

### 15.2 三分钟版本

WeKnora 原有评测已经有 Precision、Recall、MRR、NDCG、MAP、BLEU 和 ROUGE，但任务状态在进程内存中，缺少历史、逐 Case 证据和质量回归门禁。我的工作重点是围绕真实 RAG 主线补齐评测平台基础：数据集服务现在会校验五个 Parquet 文件的 ID 和关系完整性，并生成数据集 fingerprint；配置快照记录模型、分块、检索、生成、索引、代码版本和 Prompt 指纹，但不保存密钥和原文；Run/Case 数据库模型保存生命周期、四维结果、检索 PID 证据、warning 和耗时；通过 `evaluation_pid` 把临时知识库切块重新映射到数据集 passage，从而保证指标输入可审计。

在成本观测方面，统一 Wrapper 对真实 Provider 调用写入 `model_usage_events`，流式请求在结束、错误或取消时只落一条最终事件，批量 Embedding 作为一次逻辑调用并记录 item 数量。模型管理页面已经可以按模型和时间查看调用、成功/失败、Token、厂商缓存命中和平均耗时。这里需要特别说明，Token 用量不等于真实费用：当前金额、币种和价格版本只是数据契约，尚未完成生产价格表，所以没有把未知费用伪装成 0。

Prompt 优化方面，Wiki 的稳定规则、输出 Schema 和文档级上下文已经移到变化内容之前，并通过测试保证跨批次前缀字节一致；但真实命中率和费用收益还要在 Provider 环境中测量。下一步按“主线同步和迁移验证 → 真实调用验收 → 价格计算 → Embedding 缓存 → CI 门禁 → 解析引擎基线”的顺序推进。

### 15.3 个人贡献表达模板

可以根据实际分工使用以下表达：

> 我负责的是 WeKnora 评测和模型调用可观测这一条后端闭环。核心不是增加一个页面，而是把一次 RAG 评测变成可追溯的数据产品：我设计了数据集 fingerprint、配置 hash、Run/Case 持久化、`evaluation_pid` 检索证据和四维结果 Schema；同时在 Chat、Stream、Embedding、Batch Embedding 和 Rerank 的模型边界增加统一用量事件，并处理了流式取消、批量调用、Token 未上报和缓存未知等边界。当前用量和评测持久化已经有代码和测试证据，真实费用、Embedding 自有缓存和 CI 门禁仍按后续阶段建设，没有把尚未验证的收益写成已完成指标。

## 16. 最终结论

当前项目已经从“有指标但不可追溯”推进到“评测 Run/Case、配置身份、四维结果和模型用量具备持久化基础”。其中最有价值的工程变化是：

1. 评测结果从进程内存提升为数据库事实；
2. 检索结果从总分提升为可回看的 passage ID 证据；
3. 模型调用从日志提升为租户隔离的结构化事件；
4. Token、缓存和费用明确区分已知、未上报和不可用；
5. Wiki Prompt 已具备稳定前缀，为厂商原生缓存优化留下可测量基础。

但项目还不能被汇报为“全部完成”：

- 没有真实价格表，就不能说已经统计实际费用；
- 没有 Embedding Cache，就不能说已经实现向量结果复用；
- 没有 CI Workflow 和阈值文件，就不能说已经实现质量回归阻断；
- 没有统一八引擎实验和报告，就不能说已经完成解析基线；
- 启动收口不是断点续跑，多实例安全接管仍未解决。

最稳妥的阶段性结论是：**评测持久化、配置追溯、四维观测和模型调用事件已形成可继续验收的实现基础；真实成本、Embedding 缓存、CI 门禁和解析横向基线是下一阶段的明确工程计划。**
