# 课题 3：质量评测基线与成本可观测提交说明

姓名：徐博

学校：湖南第一师范

GitHub ID：yushangcan

成果类型：代码、详细设计方案与验收证据

提交日期：2026-09-14

本成果基于 WeKnora 官方主线，补齐了可复现评测、Run/Case 持久化、四维质量结果、历史比较与 CLI 退化门禁、模型调用记录与统计、Embedding 结果缓存、Wiki 稳定提示词前缀，以及 PostgreSQL/SQLite 迁移和多实例租约保护。此次提交还增加了真实 Luna Provider 的 20 问评测与 disabled/cold/warm 缓存对照，证明实现可以在真实模型链路中运行。

## 1. 仓库与版本

- 公开仓库：[github.com/yushangcan/WeKnora](https://github.com/yushangcan/WeKnora)
- 成果分支：[codex/evaluation-four-dimension-results](https://github.com/yushangcan/WeKnora/tree/codex/evaluation-four-dimension-results)
- 冻结 Tag：[rhino-2026-final-3-v2](https://github.com/yushangcan/WeKnora/tree/rhino-2026-final-3-v2)
- 元数据：[成果分支根目录 submission.yaml](https://github.com/yushangcan/WeKnora/blob/codex/evaluation-four-dimension-results/submission.yaml)
- 官方审计基线：`988cbb03`

当前材料版本使用独立 annotated Tag `rhino-2026-final-3-v2`；原有 `rhino-2026-final-3` 保留不动。Tag 冻结代码、设计、PDF 与证据，再在后续 metadata 提交更新 submission.yaml，因此成果分支会比 Tag 多一个元数据提交。Tag 内保留上一版 metadata；本版 Tag/SHA 以成果分支根目录的后续 metadata 提交为准。每阶段实现可从 `git log --oneline` 追溯。

## 2. 建议阅读顺序

1. 本页：成果范围、真实测试结果和限制。
2. [详细设计方案](详细设计方案.md)：从官方主线、数据契约、模块职责到实现取舍。
3. [验收索引](evidence/验收索引.json)：GitHub Actions 及真实 Provider 证据、测试 SHA 和文件哈希。
4. [真实 Provider 对比报告](evidence/real-provider-20260914/cache-comparison-report.md)：Luna、BGE、Redis 缓存和 D 盘 Docker 数据的实测结果。

## 3. 完成内容

| 课题要求 | 完成状态 | 主要实现或证据 |
|---|---|---|
| 固定数据、模型、分块和生成参数 | 已完成 | 数据/模型/参数指纹、config_hash、构建 SHA 和指标版本写入 Run 快照 |
| 运行与 Case 结果持久化 | 已完成 | PostgreSQL/SQLite migration、Run/Case API、重启读取、租约释放 |
| 检索、答案、成本、耗时四维结果 | 已完成 | Retrieval、BLEU/ROUGE、Usage、Cost、Timing 分开统计；未知费用保留 NULL |
| 历史比较和质量退化门禁 | 已完成 | CLI compare/gate；答案退化和 Recall 退化负向场景均能拒绝 |
| 模型调用记录和查询 | 已完成 | `model_usage_events`、summary/events API、前端 Model Usage 页面 |
| Embedding 结果缓存 | 已完成并实测 | LRU/Redis、singleflight、租户/模型配置隔离、失效和 fail-open |
| Wiki 稳定前缀 | 已完成 | 固定前缀、动态内容后置和输出结构回归 |
| CI 和数据库迁移 | 实现及合成验收完成；仓库强制门禁部分待配置 | Evaluation、Migration、Embedding Cache workflow；默认分支 schedule/required checks 未启用 |
| Luna 真实模型评测 | 已完成 | 20/20 Case 成功，真实 Chat/Embedding/Rerank 事件和 Token 用量已保存 |
| 真实缓存收益对照 | Embedding 完成；Wiki 未实测 | disabled/cold/warm 三组及 Redis 快照归档；Wiki 厂商缓存对照仍待执行 |
| 八解析引擎横评 | 选做，按要求暂缓 | 本提交不包含该横评，也不虚构结果 |

## 4. 真实 Luna 验收

真实测试使用 `gpt-5.6-luna` 作为问答模型，`BAAI/bge-small-zh-v1.5` 作为 Embedding，`BAAI/bge-reranker-base` 作为 Rerank。数据为 CMRC2018 固定语料的 848 个段落和 20 个问题。

| 组别 | 状态 | Embedding Provider | Rerank | Chat | 准备耗时 | 总耗时 |
|---|---:|---:|---:|---:|---:|---:|
| disabled-20 | 20/20 | 264 次 / 1234 项 | 20 / 600 | 21 | 366241 ms | 730307 ms |
| cold-20-final | 20/20 | 264 次 / 1234 项 | 20 / 600 | 21 | 124297 ms | 718091 ms |
| warm-20 | 20/20 | 1 次 / 1 项 | 20 / 600 | 21 | 622 ms | 341215 ms |

warm 相对 cold 的 Embedding 文本请求减少 99.92%，准备阶段耗时减少 99.50%，总耗时减少 52.48%。总耗时还包含 Rerank 和 Chat，因此不能把全部下降归因于 Embedding 缓存。三组 Retrieval 指标一致：Precision=0.7854、Recall=1、MRR=1、MAP=1、NDCG@3=1、NDCG@10=1。BLEU/ROUGE 的小幅变化来自 Luna 非确定性输出。

三组表格按测量窗口统计，包含后台调用；每组关联 Run 的问答调用为 20 次，另有 1 次 ingestion Chat。warm 窗口内剩余 1 次 Embedding 是无 run_id 的后台调用；Run 关联的 Embedding 事件实际从 cold 的 21 次降至 0。全局数据库事件由 63 条降至 42 条，Run 关联事件由 61 条降至 40 条。测试结束时 Redis 前缀匹配 1235 个键。Embedding/Rerank Provider 不返回 Token 时，系统保留不可用状态，不把未知用量记为零，也不计算无依据的费用。

真实测试证据位于 [real-provider-20260914](evidence/real-provider-20260914/)，包括汇总、用量、Provider 事件和脱敏对比报告，不包含 API Key 或原始 Prompt。

## 5. 合成 CI 和数据库验收

- [Evaluation workflow 34759714335](https://github.com/yushangcan/WeKnora/actions/runs/34759714335)：4 次 Run、8 个 Case；正常比较通过，答案和 Recall 退化门禁被拒绝，重启读回一致，终态租约释放。
- [Migration workflow 34759294412](https://github.com/yushangcan/WeKnora/actions/runs/34759294412)：PostgreSQL 升级、重复升级、回滚和恢复，SQLite 回归及租约字段检查通过。
- [Embedding Cache workflow 34756888995](https://github.com/yushangcan/WeKnora/actions/runs/34756888995)：Embedding 包测试、LRU/Redis/singleflight、命中失效和 fail-open 检查通过。

一条命令可复现合成链路：

```bash
git clone https://github.com/yushangcan/WeKnora.git
cd WeKnora
git checkout rhino-2026-final-3-v2
bash scripts/evaluation_ci.sh
```

该脚本使用自包含 Compose、PostgreSQL、Redis 和 synthetic Provider，执行构建、迁移、评测、比较、正负向门禁、重启读取和 SQL 断言。合成 Provider 用于确定工程行为，不能被解释为真实模型质量或费用结果。

## 6. 代码与测试范围

核心实现位于 `internal/evaluation/`、`internal/application/service/`、`internal/models/usage/`、`internal/models/embedding/`、`cmd/evaluation/`、`ci/evaluation/` 和前端评测/模型统计页面。主要测试包括：

```bash
go test ./cmd/evaluation ./dataset/cmrc2018/convert -count=1
python3 -m unittest discover -s ci/evaluation -p 'test_*.py' -v
cd frontend
npm ci
npm run type-check
npm run build
npm test -- src/views/evaluation/evaluationSelection.test.ts src/views/evaluation/evaluationComparison.test.ts
```

依赖 `pg_query` 的 Go 包需要 Linux CGO 环境；Windows 原生缺少 GCC 时应使用 Docker 或 GitHub Actions 验证。CMRC2018 完整数据按 `dataset/cmrc2018/README.md` 下载并验哈希，当前真实 Luna 运行使用固定 20 问验收集，不声称完成 200 问大规模质量基准。

## 7. 已知边界

- Provider 未返回价格目录或完整账单时，Cost 为 `NULL`；本提交报告 Token 和调用事实，不伪造金额。
- Luna Chat 的厂商 Token Cache 与 WeKnora Embedding Redis Cache 是两套机制；`cache_status=mixed` 不等同于 Embedding 命中率。
- 历史 GitHub workflow 已在成果分支验证；默认分支定时运行和 required checks 仍需仓库维护者配置，不能仅凭 workflow 文件声称已启用分支保护。此次材料更新没有把历史 Actions 的结果记为新 Tag 的 CI。
- 真实测试规模为 20 问、三组各一次；不能代表 200 问完整基准、生产收益或统计显著的质量改进。Wiki 厂商前缀缓存收益、多实例运行中故障演练和模型统计页面浏览器验收尚未完成。
- 真实运行构建 SHA 为 `5ee7afee78c277102c901e886464d6160f26d9af`，原报告 `vcs_modified=true`、`reproducibility=partial`，且未保存构建时 dirty diff。该限制如实保留，当前提交后的干净工作区不改变历史构建状态。八解析引擎横评按之前决定暂缓。
- 测试证据已脱敏并提交到本目录；临时密钥、私有会话和 Docker 运行目录均未提交。

## 8. 提交说明

本次提交将代码、设计方案、合成 CI 证据和真实 Luna 验收证据统一整理到成果分支，并推送到个人远端仓库。审阅时可先查看 `submission.yaml`、本页的真实测试表和详细设计，再按阶段 Commit 检查具体实现。所有结果均区分了“代码已实现”“合成环境已验证”和“真实 Provider 已验证”三种证据层级。
