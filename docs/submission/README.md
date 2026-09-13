# 课题 3 质量评测基线与成本可观测提交说明

姓名：徐博

学校：湖南第一师范

GitHub ID：yushangcan

成果类型：代码，附设计方案与验收说明

提交日期：2026-09-13

本成果基于 WeKnora 官方系统，完成评测 Run 与 Case 持久化、四维结果、历史比较与 CLI 质量门禁、模型调用数据库记录及统计页面、Embedding 结果缓存和 Wiki 提示词稳定前缀。核心模块与合成端到端链路已实现；真实 Provider 的费用及缓存收益仍待实测，详见下方验收表。

## 1 仓库与冻结版本

- 公开仓库：https://github.com/yushangcan/WeKnora
- 成果分支：https://github.com/yushangcan/WeKnora/tree/codex/evaluation-four-dimension-results
- 冻结 Tag：`rhino-2026-final-3`
- 冻结源码：https://github.com/yushangcan/WeKnora/tree/rhino-2026-final-3
- 完整 Commit SHA：见分支根目录 [submission.yaml](../../submission.yaml)

按实战指引，先冻结含源码与设计材料的提交并推送 annotated Tag，再单独提交 submission.yaml。因此 metadata 提交会晚于 Tag，冻结快照内没有该 YAML 属于规定流程；请在成果分支根目录读取元数据。Tag 不移动、不覆盖。

官方源码审计基线为 `988cbb03`。本分支包含 upstream 同步历史，个人实现范围以本说明和详细设计的阶段提交为准，未合入 Tencent 官方主线。

## 2 建议阅读顺序

1. 本页：成果范围、验收状态与运行方法。
2. [详细设计方案](详细设计方案.md)：从官方流程开始说明架构、数据、模块、取舍和代码映射。
3. [原始证据索引](evidence/验收索引.json)：实际运行代码 SHA、Actions 链接、归档文件及 SHA-256。
4. [历史总设计](../WeKnora官方主线到优化完成详细设计方案-v1.md) 和 [线上收尾记录](../优化闭环与线上交付记录-2026-09-13.md)：分阶段演进与线上修复。

附邮件 PDF 位于仓库 `output/pdf/`，内容来自本提交说明和详细设计。原始需求 DOCX 属于评审依据，未作为本人作品上传。

## 3 逐项验收状态

| 课题要求 | 当前状态 | 审阅依据 |
|---|---|---|
| 固定数据 模型 分块参数并记录 | 已实现 | 数据指纹 config_hash 构建 SHA 与配置快照 |
| 结果落数据库 重启可查询 | 合成 E2E 已验证 | Run Case API SQL 检查 重启前后结果一致 |
| 同时给出检索 答案 成本 耗时 | 已实现 | baseline.json 包含四维结果；无金额依据时 NULL |
| CI 定时与退化门禁 | 实现及合成门禁验证；部署配置部分待完成 | workflow 声明定时；优化分支 push 已运行；默认分支与 required checks 待启用 |
| 召回下降时指出指标并失败 | 合成 E2E 已验证 | Recall 从 1 降至 0；gate 日志明确输出 recall delta -1 |
| 模型及时间区间调用 缓存 费用查询 | 已实现 数据库及前端检查通过 | model_usage_events summary/events ModelUsagePanel；真实 Provider 页面演示待补 |
| Embedding 相同输入复用 | 已实现 专项 CI 通过 | LRU Redis singleflight 模型失效 fail open benchmark |
| Wiki 固定前缀优化 | 已实现 前缀与输出回归通过 | prompts_wiki.go 及对照 fixture |
| 缓存优化前后实测收益 | 尚未完成课题要求的真实场景实测 | Fake Provider benchmark 不代替索引与 Wiki 实测 |
| 八解析引擎横评 | 选做 暂缓 | 当前提交未包含 |

## 4 实际验证记录

最终自包含 Evaluation 运行 [34759714335](https://github.com/yushangcan/WeKnora/actions/runs/34759714335) 成功，实际编译代码 `f0a96a013269e35c2b265181fa5c88af8f3fcdf2`，使用指标版本 v3。报告确认 4 次 Run、8 个 Case；正常比较通过，错误答案和召回退化门禁均被正确拒绝；重启后结果一致，所有终态租约已释放。召回负向日志明确输出 `recall delta -1.000000`。源码和文档冻结提交会晚于该测试代码提交。

[Embedding Cache 34756888995](https://github.com/yushangcan/WeKnora/actions/runs/34756888995) 成功，代码 `5cad1109`，包括 Embedding 包测试与 disabled/cold/warm benchmark。该 benchmark 使用 Fake Provider：disabled 和 cold 为 1 provider_call/op，warm 为 0.0000005 provider_calls/op；这是预热后摊销的函数级计数，不是生产费用或延迟收益。

[Evaluation Migrations 34759294412](https://github.com/yushangcan/WeKnora/actions/runs/34759294412) 成功，代码 `8945b5b2`，包含 PostgreSQL migration 生命周期及租约、SQLite 回归，并验证空重排结果与未执行重排的不同指标行为。首次与重复 up 均为 95，down 后 94，再恢复至 95。此前运行 34757464367 的迁移证据也保留在索引中。

本地检查包括 Evaluation CLI、CMRC 转换器五项离线回归及官方文件转换测试，前端类型检查和生产构建、评测选择与比较六项测试。新增召回场景后的 Python harness 为 12 项测试通过。Linux CI 覆盖原生 Windows 环境无法编译的 pg_query CGO 相关路径。

## 5 一条命令复现合成验收

前提：Git、Bash、可用 Docker Engine、支持 `up --wait` 的 Compose v2；首次构建能下载镜像、Go 模块与构建依赖。无需真实模型凭据，也无需提前部署 App。推荐 Linux，Windows 可使用 Docker Desktop 的 Linux 容器模式与 Git Bash。

```bash
git clone https://github.com/yushangcan/WeKnora.git
cd WeKnora
git checkout rhino-2026-final-3
bash scripts/evaluation_ci.sh
```

最后一条命令执行构建、数据库迁移、临时用户认证、四次真实评测、正常比较、答案与召回负向门禁、App 重启后读取及 SQL 验证。成功退出 0；产物保存在 `tmp/evaluation-ci/` 下的本次运行目录。负向 gate 的非零退出是预期结果，由外层脚本核对后继续。

固定合成用例的正常 Recall、MRR、MAP 应为 1，召回退化 Run 的 Recall 应为 0；金额应为 NULL。Run ID、时间戳、耗时和报告内构建 SHA 随运行变化，不要求与旧 artifact 字节完全相同。新增代码或错误统计导致正常质量低于下限时，整个 CI 必须失败。

需要的主要文件是 baseline.json、candidate.json、degraded.json、retrieval-degraded.json、comparison.json、retrieval-regression.json、run-status.json、restart-status.json、database-check.txt 与 status.txt。线上 artifact 保留 14 天，本提交的 evidence 目录额外保存精选原始文件和哈希。

## 6 构建与专项测试

完整环境构建由上述脚本负责。前端单独检查需要 Node.js 与项目依赖；Go 定向测试需要 Go 1.26，依赖 pg_query 的包应在 Linux CGO 环境执行。

```bash
go test ./cmd/evaluation ./dataset/cmrc2018/convert -count=1
python3 -m unittest discover -s ci/evaluation -p 'test_*.py' -v
cd frontend
npm ci
npm run type-check
npm run build
npm test -- src/views/evaluation/evaluationSelection.test.ts src/views/evaluation/evaluationComparison.test.ts
```

CMRC2018 的下载校验与转换参见 [数据说明](../../dataset/cmrc2018/README.md)。默认 200 问和 848 上下文是数据准备结果，不是已经完成的真实模型评测。

## 7 已知问题与后续工作

- 未完成真实 Provider 的费用、Token、cache 字段与模型页面联合验收，也没有可作为账单依据的真实费用数据。
- 未完成真实重建索引的 Embedding 调用降幅，以及 Wiki 生成的厂商缓存命中提升实测。
- 默认分支仍为 main；成果 workflow 在优化分支。GitHub schedule 需合入默认分支才运行，本次查询 main 未设分支保护，required checks 尚未启用。
- 合成 CI 验证完成后重启读取，未覆盖双 App 运行中崩溃演练；租约只收敛中断状态，不实现自动断点续跑。
- 调用记录写入失败不影响原请求，当前没有持久队列补偿；计价支持仍受币种、计费单位和最小货币精度限制，异步账单对账未实现。
- 八解析引擎横评属于选做，本次暂缓。

后续按真实 Provider 联调、Embedding 三组实测、Wiki 对照、默认分支门禁启用和多实例演练顺序推进；每项继续保留数据、配置、代码 SHA 和原始证据。
