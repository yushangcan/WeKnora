# Embedding 结果缓存运行说明

Embedding 结果缓存位于模型调用观测包装器外层。命中时直接返回已校验的向量，不伪造 Provider Usage Event；未命中时仍沿用原 Provider、限流、重试和返回值。

## 模式

| 条件 | 后端 | 行为 |
|---|---|---|
| `WEKNORA_EMBEDDING_CACHE_ENABLED` 为空或 false | No-op | 完全透传 Provider，不读写缓存 |
| 开启且 Redis 客户端可用 | Redis | 跨进程复用、TTL、批量读写和 Redis token lock |
| 开启但没有 Redis 客户端 | Lite memory LRU | 有界条目数、TTL、单进程复用；进程重启后丢失 |

## 配置

```text
WEKNORA_EMBEDDING_CACHE_ENABLED=false
WEKNORA_EMBEDDING_CACHE_TTL=168h
WEKNORA_EMBEDDING_CACHE_MEMORY_MAX_ENTRIES=2000
WEKNORA_EMBEDDING_CACHE_PREFIX=weknora:embedding:v1
WEKNORA_EMBEDDING_CACHE_DISTRIBUTED_LOCK_ENABLED=true
WEKNORA_EMBEDDING_CACHE_LOCK_LEASE=30s
WEKNORA_EMBEDDING_CACHE_LOCK_WAIT=2s
WEKNORA_EMBEDDING_CACHE_LOCK_RENEWAL=10s
```

跨进程锁只对 Redis 后端生效。Leader 在锁租约内调用 Provider、校验向量并写入缓存；Waiter 获得锁后先重新读缓存，避免重复调用。锁等待有上限，Redis 故障、锁超时或锁服务不可用时会 Fail Open，直接调用 Provider。关闭分布式锁后仍保留进程内 singleflight。

## Key 和失效

Key 绑定缓存版本、租户、模型 ID、模型更新时间/配置指纹、Provider、Provider 模型名、向量维度、截断参数和原始 UTF-8 文本字节。文本不会被自动 trim、大小写归一化或 Unicode 归一化，Key 和日志不包含原文。

模型配置、维度、Provider 或缓存格式变化必须产生新的 Key 版本。切换模型通常还需要重新构建知识库向量，不能只依靠缓存失效解决旧向量问题。测试只能删除自己的 prefix，禁止对共享 Redis 使用 `FLUSHDB`。

## 观测口径

代码级快照 `embedding.CacheMetrics(cache)` 提供以下应用缓存计数：

- `requests`：缓存包装器收到的逻辑请求数；
- `hit_items` / `miss_items`：输入文本条目的命中和未命中数，批内重复按输入位置计数；
- `provider_requests` / `provider_items`：实际发给 Provider 的请求数和文本数；
- `get_errors` / `set_errors`：缓存读写错误；
- `invalid_entries`：损坏、维度不符、空向量或非有限向量；
- `singleflight_waits`：同一进程等待已有 Miss 计算的次数；
- `lock_waits`：跨进程锁超时或 Redis 协调失败后触发 Fail Open 的次数。

这些字段是 WeKnora 应用缓存指标，不能与 Provider Prompt Cache 的 `cache_read_tokens`、`cache_hit_calls` 或 `cache_hit_rate` 混合。当前快照尚未接入独立 HTTP 仪表盘，后续应按租户、模型、缓存后端和缓存版本聚合后再展示。

## 正确性和降级

- 只缓存成功且通过维度、空值、NaN/Inf 检查的向量；错误、取消、超时和数量不匹配结果不缓存。
- 批量请求只向 Provider 发送 Miss，返回时恢复原始顺序；Pool 子批次不会重复产生逻辑 Usage Event。
- 缓存读写异常不改变 Provider 结果，缓存只提供优化，不是业务正确性的前置依赖。
- 不同租户、模型版本、维度和原始文本不得互相命中。
- 真实多实例收益、冷/热/禁用耗时和 Provider request reduction 必须通过独立实验确认，代码测试不能替代部署验收。
