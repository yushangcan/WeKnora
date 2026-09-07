# WeKnora 质量评测与成本治理整体设计方案 v3

> 这是后续开发的主计划和验收索引。审计基线为 C:\Users\25515\Desktop\WeKnora，分支 codex/evaluation-four-dimension-results，当前实现检查点为 `ff9b2fce`。需求来源为 C:\Users\25515\Desktop\weknora需求文档.txt；历史计划位于 C:\Users\25515\Desktop\优化方案\。附件中的规划是需求和历史记录，不能替代当前源码审计。

## 1. 目标与边界

目标是形成两个可长期运行的闭环：

1. 固定数据集、模型、分块和检索参数 -> 可追溯 Evaluation Run -> Run/Case 的质量、用量、成本状态、耗时证据 -> 数据库历史与对比 -> 固定命令复现 -> CI 回归门禁。
2. Chat/Stream/Embedding/Rerank/Wiki -> 一次真实 Provider round-trip 一条结构化事件 -> 按租户、模型、Provider、来源和时间聚合 -> Token、Provider Cache、费用状态可解释 -> Embedding 应用缓存和 Wiki Prompt 前缀优化。

硬边界：

- 评测复用真实 RAG pipeline，不另写只测向量库的流程。
- 不改变 Precision、Recall、NDCG、MRR、MAP、BLEU、ROUGE 公式；改变输入语义必须升级 metric version。
- Token、缓存 Token、调用次数不是货币金额；未知金额为 NULL + unavailable，不能写 0。
- WeKnora 应用 Embedding Cache 和 Provider Prompt Cache 分开统计。
- 不保存 Prompt、答案、文档正文、API Key、Authorization Header 或完整 Endpoint。
- 每个代码目标单独 Git 检查点；不使用 git add .，不删除旧实验结果，不用删除文件解决冲突。
- 单元测试、模拟 Provider、真实 Provider、真实数据库、Docker、Linux + CGO、CI 的证据必须分开表述。

最终完成定义：

1. 干净环境可用一条命令提交评测、轮询、保存报告并读取终态；
2. Run 配置、数据集、代码和模型身份可追溯，敏感信息被排除；
3. Run 和 Case 在数据库中持久化，重启后可查询；
4. 可按历史 Run 比较质量、用量和耗时，并判断是否兼容；
5. 模型页面展示调用事实；未知 Token、缓存和费用显示 —；
6. Embedding 冷/热/禁用三组实验有真实对照；
7. Wiki Prompt 结构优化有功能回归和 Provider 命中证据；无 Provider 字段时只声明前缀稳定；
8. CI 使用固定配置、基线和容差阻断质量退化；
9. 可选的八解析引擎横评有统一语料、指标和报告。

## 2. 当前源码状态

| 领域 | 当前实际状态 | 未完成边界 |
|---|---|---|
| Dataset | dataset.go 校验五个 Parquet 文件并生成 manifest/fingerprint | 需要真实端到端报告 |
| Evaluation | Run/Case observer、四维结果、config snapshot、metric version 已有 | 需补 fresh/upgrade/down 和真实重启证据 |
| Persistence | evaluation_runs、evaluation_run_cases、分页、comparison、租户条件已有 | migration 需同步上游后重验 |
| Recovery | 启动会把所有 pending/running Run 关闭为失败/partial | 这是单实例假设，多实例需 owner/lease/heartbeat |
| Model Usage | Chat/Stream/Embedding/Rerank wrapper、model_usage_events、汇总 API、模型页、ProviderUsage Scope、Provider Request ID、000093 migration、PricingResolver 已有 | Provider-specific Embed/Rerank/VLM/ASR adapter 尚未完整接入；PricingResolver 尚未接入持久化金额回写；真实 Provider 费用仍缺 |
| Embedding Cache | Redis/Lite LRU、TTL、租户/模型隔离、批内去重、顺序恢复、singleflight、指标、Redis token lock、Fail Open、冷/热/禁用 benchmark、独立 Redis Client 协调测试、模型版本失效测试已有 | Linux CI 和代码级测试已补；真实 Provider 延迟/调用量、真实 Redis 多实例部署和故障报告仍待验证 |
| Wiki Prompt | 所有主要模板有解析/占位符/稳定前缀测试；Deduplication 已完成一次最小重排；有脱敏 Provider Cache 对比报告值 | Provider 命中证据、真实小样本质量和其余模板收益未验证 |
| CLI | cmd/evaluation 可 POST、轮询、保存报告；`compare` 读取历史 Run；`gate` 按质量容差阻断 | 需认证后的真实 Provider 运行和 CI artifact 验证 |
| CI/Parser | 已有定时复现 workflow、缺配置 skipped artifact、质量 gate、Embedding Cache Linux 测试/benchmark workflow；八解析器横评尚未开始 | 评测 workflow 尚未在本项目真实凭据环境运行；Parser 仍是可选项目 |

### 2.1 目标架构

评测流程：

POST /api/v1/evaluation
-> Handler
-> EvaluationService
-> LoadDataset（ID/关系/完整性/fingerprint）
-> BuildEvaluationConfig（Dataset、模型、分块、检索、生成、代码身份）
-> CreateRun(pending)
-> 写完整 corpus 到临时 KnowledgeBase，等待索引 ready
-> 每个 Case 派生 Context(run_id, case_id, phase)
-> 真实 KnowledgeQAByEvent/RAG pipeline
-> Evaluation Meter + Model Usage Recorder
-> SaveCaseProgress
-> 聚合并保存 terminal Run
-> GET /api/v1/evaluation?task_id=...

Run/Case ID 只能在 Context 中传播，不用全局当前 Run/Case。Collector 必须并发安全，Snapshot 必须复制内部切片。

模型包装目标：

Provider
-> Provider usage adapter（可选 Scope 上报）
-> 原有重试/限流/并发/Langfuse/debug
-> Evaluation Meter
-> Model Usage Recorder
-> Embedding Result Cache（在 Usage 外层）
-> 业务调用

Cache Hit 不伪造 Provider Usage Event；Embedding Pool 子批次不能和逻辑批量调用重复计数。

## 3. 数据契约

### 3.1 Dataset 与可复现配置

默认数据集包含 queries.parquet、corpus.parquet、qrels.parquet、qas.parquet、answers.parquet。Manifest 保存文件名、大小、SHA-256、记录数、Dataset ID/Version、组合 fingerprint、QueryCount、CorpusCount、CaseCount、IngestionMode，不保存绝对路径。

evaluation-config/v2 至少保存 Dataset manifest/fingerprint、三类模型身份与更新时间、Chunk/检索/生成参数、Prompt/Context/Fallback fingerprint、向量和关键词索引开关、应用版本、Commit SHA、工作树状态、metric/result version、config_hash。

排除 API Key、Secret、完整 Header、完整 Prompt、完整 Endpoint、正文。缺少 Commit 或 Provider 配置时标记 reproducibility=partial。

### 3.2 Run/Case 与 Model Usage

evaluation_runs 保存 Run/Tenant/KnowledgeBase 身份、Dataset/Config/Metric/Result version、模型、状态、进度、错误、配置和聚合 Result snapshot、生命周期时间。

evaluation_run_cases 保存 Run+Case、状态、耗时、Usage、warnings、Case Result snapshot、ground-truth PID、检索和 rerank PID 排名、无法映射数量，以及问题/参考答案/生成答案 fingerprint。

Run Snapshot 不复制完整 Cases；读取时先读 Run，再稳定排序组装 Case。所有查询强制 tenant_id 条件。旧 API metric 字段继续保留。

一条 ModelUsageEvent 代表一次真实 Provider round-trip，至少包含 call_id、tenant/model/provider/type/operation/source、started/completed/duration/success/error、item_count、nullable Token/Prompt Cache/Cost、evaluation_run_id、evaluation_case_id、session_id、trace_id。

约定：重试的每次实际 Provider 尝试需要可追踪；Stream 最终只落一条事件；Batch Embedding 计一次逻辑调用；未上报 Token 不等于 0；未上报缓存不等于 miss；tokens_reported_calls 和 cache_reported_calls 单独统计。

### 3.3 成本

来源分三层：

1. provider_reported：Provider 明确返回金额、币种、口径和可选 request ID；
2. catalog_calculated：版本化价格目录按明确计费单位计算；
3. billing_reconciled：账单/Usage API 异步对账。

调用 Scope 传递 ProviderUsage{Tokens, BillableUnits, Amount, AmountMinor, RequestID, RawSource}，避免修改 Embedder/Reranker 公共接口。当前 Chat/Stream Wrapper 已复制 Scope，Provider Request ID 已持久化；Embedding/Rerank 的 Provider-specific Scope 适配仍待补齐。PricingResolver 已支持版本化目录、Provider minor amount 优先和精确 minor-unit 计算，但尚未把解析结果自动回写 ModelUsageEvent。金额使用 Decimal 或 minor-unit 整数，不用 float64 累计。未知、缺币种、混币种或单位不完整时 amount=null，状态为 unavailable/partial/pending。自托管模型可为 not_applicable，但不等于业务成本为零。

## 4. API、CLI、前端

保留 POST /api/v1/evaluation 与 GET /api/v1/evaluation?task_id=...。历史接口为 /evaluation/runs、/evaluation/runs/{run_id}、/evaluation/runs/{run_id}/cases、/evaluation/comparison。列表支持 page/page_size、status、dataset_id、config_hash、模型和 RFC3339 时间过滤；排序固定，page_size 有上限。

cmd/evaluation 从环境读取服务地址、API Key、Dataset/KB/模型参数，提交、轮询、保留最后完整响应并输出 Run ID/config hash/metric version/报告路径；`compare` 调用历史 Run comparison；`gate` 读取 comparison artifact 做质量回归阻断。凭据不写日志或报告。可复现表示输入身份可追溯，不表示 Provider 外部状态和非确定性输出可自动还原。

前端分为 Run 列表、Run 四维详情、Case 证据；模型页分汇总卡片、按模型聚合和调用明细。未知 Token、缓存、费用显示 —，不展示完整 endpoint、Header、密钥或 Prompt。

## 5. Embedding Cache

Cache Key 绑定 schema_version、tenant_id、model_id、model_updated_at/config fingerprint、Provider、provider model、dimension、截断/归一化参数和原始 UTF-8 文本字节。不隐式 trim、大小写或 Unicode 归一化。

算法：Get/GetMany -> Hit 返回副本 -> Miss 进入 singleflight -> 只发送 Miss -> 校验数量/维度/有限浮点/顺序 -> Set/SetMany。错误、取消、超时、空向量、维度错、NaN/Inf、数量不匹配不得缓存。Lite 用 LRU+TTL+容量上限；Redis 用独立 namespace；缓存故障 Fail Open。

Redis 模式已经增加 Redis token-owned lock：Leader 调 Provider、校验、写缓存、释放锁；Waiter 有界退避读缓存；Redis/锁异常或超时 Fail Open。Lite 模式只使用进程内 singleflight。代码级协调测试已存在，但没有真实多实例部署报告前，不宣称生产吞吐收益。

新增应用缓存指标：requests、hit_items、miss_items、provider_requests、provider_items、get/set_errors、invalid_entries、singleflight_waits、lock_waits。不能复用 Provider Cache 字段。

## 6. Wiki Prompt、CI 与解析器

固定规则、输出 Schema、稳定语言/候选 slug/共享 source context 放前，动态 chunks、页面正文和批变量放后。按 call_purpose + 不可逆 prompt_prefix_fingerprint 建 cohort；不记录正文。一次只改一个模板：先字节级前缀测试和 Fake Provider，再做真实小样本；Provider 没有缓存字段时只能报告结构稳定。优先 Citation、Candidate、Summary，再处理 Taxonomy/Deduplication/Index。

CI 前置条件：固定 Dataset fingerprint、模型/分块/检索/生成配置、Secret/额度、baseline artifact、metric/result version、非确定性容差、失败重试。流程为固定提交 -> PostgreSQL/Redis/Stub Provider -> migration -> cmd/evaluation -> 保存 JSON artifact -> Case 对齐比较。当前 `gate` 默认检查全部已持久化 Retrieval/Answer 质量指标，也可用环境变量收窄指标集合和设置容差；成本/耗时不参与质量阻断；配置不兼容为 incompatible；失败运行不得覆盖 baseline。

八解析引擎是独立可选项目：统一 Adapter、固定 20~50 份代表性语料、记录 parser 版本/耗时/失败率/输出大小/结构和文本质量/下游 Embedding token，保留原始输出和人工评分依据。

## 7. 分阶段开发顺序

### Phase 0：基线

确认 branch、HEAD、工作区、上游 migration；建立 backup 分支；跑目标测试和 git diff --check，记录 Windows CGO、Docker、Provider 限制。

### Phase 1：评测正确性

完整 corpus、PID 随 chunk 传播、索引 ready 校验、临时资源统一清理、fresh/upgrade/down migration、认证后的真实 E2E 和重启读取。

### Phase 2：Usage/Cost

先传递 Aliyun/Volcengine Embedding、Aliyun/Jina/Zhipu Rerank 的已解析 usage，再加 Provider request ID，最后实现版本化 PricingResolver 和金额精度；可选补 VLM/ASR。

### Phase 3：Cache

命中指标、Redis 多实例锁、Redis/Lite/No-op 代码路径、冷/热/禁用 benchmark、独立 Redis Client 协调测试、模型版本失效测试已完成。下一步是在 Linux CI 之外补真实 Redis 多实例、Provider 调用量/延迟和故障降级报告，验证 Provider Event 口径。

### Phase 4：Wiki

完善高重复模板的前缀测试；一次只改一个模板；按 Provider 字段和功能质量发布对照报告。

### Phase 5：CI

baseline comparison command、定时 workflow、质量阻断 CLI 和 Embedding Cache Linux 测试/benchmark workflow 已提交；评测定时 workflow 在缺少服务地址、租户、API Key 或数据集配置时只生成 skipped artifact。仍需在具备真实服务和 Provider 凭据的 CI 环境执行一次，核对 migration、Run/Case、报告 artifact 和 gate 的现场证据。

### Phase 6：Parser（可选）

Adapter 合约、代表性语料 benchmark、质量基线报告。

已完成的独立 Commit（按开发顺序）：

- `6cc79b73 feat(embedding-cache): add cache metrics`
- `7bb5146e feat(embedding-cache): coordinate distributed misses`
- `00eb0d61 test(wiki): cover all cacheable prompt prefixes`
- `0f034129 perf(wiki): stabilize one measured prompt prefix`
- `7b61bf00 test(wiki): add provider cache comparison report`
- `7b4fa365 feat(model-usage): propagate provider usage scope`
- `92929af6 feat(model-usage): add provider request identity`
- `160fd843 feat(model-usage): add versioned pricing resolver`
- `d299ce97 feat(evaluation): add baseline comparison command`
- `8aa255a3 ci(evaluation): run scheduled reproducibility check`
- `ff9b2fce ci(evaluation): block quality regression`
- `a9b8e76f test(embedding-cache): add cold warm disabled benchmark`
- `8adeb9db test(embedding-cache): verify independent redis clients`
- `c51d7fa8 test(embedding-cache): verify model version invalidation`
- `e2685d6e ci(embedding-cache): run linux validation`

尚未完成且不能按已完成汇报的证据项：

- `fix(evaluation): preserve passage ids through chunking`：需先确认当前代码是否仍有缺口，再单独提交。
- `test(evaluation): cover fresh upgrade rollback migrations` 与认证后的真实 E2E：本机 Windows CGO/Provider/数据库条件不足，需 Linux + CGO、PostgreSQL/Redis 或 CI。
- Provider-specific Embedding/Rerank usage adapter、PricingResolver 到 ModelUsageEvent 的金额回写、异步账单对账。
- Embedding 冷/热/禁用真实 Provider 对照、真实 Redis 多实例部署、Wiki Provider cache read/write 真实字段和质量对照。
- 八解析引擎横评仍是可选 Phase 6。

## 8. 验收、风险与 Git 规程

| 层级 | 必测内容 | 证据 |
|---|---|---|
| Go | Dataset、Config、Collector 并发、Wrapper、Cache Key、Prompt | go test / race |
| Repository | NULL、租户隔离、分页、Run/Case 组装、级联 | SQLite/PostgreSQL |
| Migration | fresh/upgrade/down、旧数据保留 | migration log/SQL |
| API/CLI | POST、轮询、历史、Case、comparison、认证 | httptest/现场 JSON |
| Docker/Provider | health、真实事件、缓存和 usage 字段 | compose + 脱敏响应 |
| E2E | 同配置重复、只改一个参数、重启后查询 | 报告 + DB 查询 |
| CI | baseline、容差、artifact、阻断 | workflow run |

主要风险：当前启动恢复是全局关闭非终态 Run 的单实例实现，多实例需 owner/lease/heartbeat；Cache Key 漏配置必须 schema/version 化；Redis 故障必须 Fail Open；Batch 顺序和 Pool 重复事件用表驱动测试；Provider 无 cost 保持 amount=null；混币种按币种分组；Prompt 单模板单 Commit；migration 先同步上游再取号；无真实凭据时明确 E2E blocked，不造指标。

每个 Commit：确认 status/HEAD -> 单目标修改 -> gofmt/前端检查 -> 目标测试 -> 回归测试 -> diff --check -> 敏感信息审查 -> 记录真实/模拟/未验证边界 -> 按流程提交。
