# 检视报告

## 概要

检视范围：my-cpa-stats-plugin 全部新增代码（16 个文件），对照 plan 和 research 文档。整体架构清晰，模块划分合理，数据流简洁。存在一个并发安全阻塞问题和若干设计/正确性建议项。

## 需求对齐

- 核心需求（UsagePlugin 采集 → 多窗口聚合 → Management API 查询）完整实现。
- 7 条 API 路由全部就位，配置项与 plan §8 一致。
- 已知偏差均在 summary 中记录：`package main`（工具链限制）、`from`/`to` 未实现（MVP）、docs 合并到根 README。
- plan §4.4 提到 series 查询支持 `from`/`to` 时间范围过滤，当前未实现。作为 MVP 可接受，但应在 README 已知限制中注明。

## 阻塞问题

| ID | 位置 | 问题 | 建议 |
| --- | ---- | ---- | ---- |
| B-1 | `plugin/aggregator/aggregator.go:115-127` | **Snapshot() 存在数据竞争**。`Snapshot()` 在 RLock 下复制 map，但 map value 是 `*Bucket` 指针——复制后 snapshot 与 live buckets 共享同一对象。释放 RLock 后，management handler 读取 `b.Count`/`b.SumLatency` 等字段时，`ingestLoop` 可能正通过 `Accumulate()` 并发写入同一 Bucket。这是经典的 read-write data race，`-race` 下必现。`statsByModel`/`statsByAuth` 中 `mergeBuckets(existing, b)` 也读取 live bucket 字段。 | 在 `Snapshot()` 中对每个 Bucket 做值拷贝（`cp := *v; cp[k] = &cp`），使 snapshot 与 live 数据完全隔离。reservoir slice 也需浅拷贝（`copy`）。或者为 Bucket 加独立 RWMutex，但值拷贝更简单且符合"简单优先"原则。 |

## 建议修改

| ID | 位置 | 问题 | 建议 |
| --- | ---- | ---- | ---- |
| S-1 | `plugin/aggregator/bucket.go:123-129` | `sortDurations` 使用插入排序，O(n²)。reservoir 容量 1024 时，每次 `Percentile()` 调用约 50 万次比较。management 查询路径上会频繁触发。 | 改用 `slices.Sort(sorted)`（Go 1.21+，项目 go 1.26），一行替换，O(n log n)。 |
| S-2 | `plugin/usage_handler.go:11` | `handleUsage` 读取 `p.cfg.Enabled` 和 `p.agg` 未持锁。若 host 并发调用 `plugin.reconfigure` 和 `usage.handle`，存在 race。 | 在 `handleUsage` 入口加 `p.mu.Lock()`/`RLock()` 读取 cfg 和 agg 引用，或改用 `atomic.Pointer` 存储 agg 引用。 |
| S-3 | `plugin/management_handler.go:281-295` | `mergeBuckets` 不合并 reservoir 数据，导致 by-model/by-auth 的 P50/P95 始终为 0。用户查询这两个端点时看到分位数缺失会困惑。 | 方案一：合并时拼接两个 reservoir（截断到 reservoirSize）。方案二：在 API 响应中对 merged 结果不输出 P50/P95 字段（`omitempty`），避免误导。方案三：在 README 中注明 by-model/by-auth 不含分位数。 |
| S-4 | `plugin/management_handler.go:83-115` | `statsOverview` 仅聚合 1m 窗口数据。用户调用 overview 期望看到全局概览，但实际只反映最近 1 分钟。 | 改为聚合 24h 窗口（或最大可用窗口），或增加 `window` 查询参数让调用方选择。 |
| S-5 | `plugin/lifecycle.go:41-43` | `configure` 在 `p.started == true` 时仅更新 `p.cfg` 就返回。运行中的 aggregator 仍使用旧的 cardinality/retention，persist loop 仍用旧 interval。`plugin.reconfigure` 实质上是空操作。 | 如果 host 会调用 reconfigure，需要实现热更新逻辑（重建 aggregator 或更新参数）。如果确认 host 不会在运行期 reconfigure，在代码中加注释说明，并在 reconfigure 时 log 一条 "reconfigure ignored, restart required" 提示。 |
| S-6 | `plugin/usage_handler.go:44` | `identityKey` 在 AuthID 为空时回退到原始 `rec.APIKey`。该 key 会通过 `/stats/keys` 端点暴露给 management API 调用方。 | 对 APIKey 做截断或哈希处理（如取前 8 字符 + `...`），避免完整密钥出现在查询响应中。 |
| S-7 | `plugin/persist/persist.go:98-103` | plan §6.2 要求写入后 fsync 再 rename。当前实现直接 rename，掉电时 tmp 文件可能不完整（虽然 rename 后旧文件仍安全）。 | 在 `os.WriteFile` 后、`os.Rename` 前，打开 tmp 文件调用 `f.Sync()`。对 stats 数据非关键，但符合 plan 约定且成本极低。 |
| S-8 | `plugin/aggregator/aggregator.go:99-113` | `evictOldest` 每次淘汰扫描整个 map（O(n)）。cardinality_limit=50000 且高基数流转时，每次新 series 插入都触发全量扫描。 | 当前规模下可接受（50K 次比较 < 1ms）。如果后续观察到性能问题，可维护一个按 LastSampleAt 排序的最小堆。暂记录备忘。 |

## 非阻塞问题

| ID | 位置 | 问题 | 建议 |
| --- | ---- | ---- | ---- |
| N-1 | `plugin/aggregator/aggregator_test.go:210` | `TestConcurrentIngest` 依赖 `time.Sleep(50ms)` 等待 channel 消费完毕。CI 高负载下可能 flaky。 | 改为轮询 Snapshot() 直到 total == 1000（带超时），或使用 `IngestDirect` 避免异步。 |
| N-2 | `plugin/management_handler.go:30-37` | `ManagementRoute.Handler` 全部为 nil。C ABI 模式下 host 通过 `management.handle` 方法路由，Handler 字段应被忽略。但如果 SDK 序列化时检查 nil interface 可能产生问题。 | 确认 SDK 对 nil Handler 的处理。如有风险，提供一个 no-op handler 实现。 |
| N-3 | `plugin/envelope.go` | envelope 格式（`ok`/`result`/`error`）与 research §4 描述的（`id`/`ok`/`payload` base64）不同。需确认与实际 host SDK 的 C ABI 协议一致。 | 对照 `examples/plugin/codex-service-tier/go/main.go` 的实际 envelope 实现验证。如 host 期望 base64 payload + id 字段，需调整。端到端验证阶段必须覆盖。 |
| N-4 | `plugin/capabilities.go:45` | `lifecycleRequest.ConfigYAML` 类型为 `[]byte`。Go JSON 对 `[]byte` 使用 base64 编解码。如果 host 发送的是 JSON string（非 base64），反序列化会失败。 | 确认 host SDK 对 ConfigYAML 的序列化方式。如果是 plain string，改为 `string` 类型。 |
| N-5 | `plugin/management_handler.go:312-328` | `parseIntDefault` 手动解析整数，不处理溢出。极大输入值会 wrap around。 | 改用 `strconv.Atoi` + 范围检查，更清晰且安全。 |
| N-6 | `plugin/lifecycle.go:83-93` | `shutdown()` 持有 `p.mu` 期间调用 `p.wg.Wait()`。当前 goroutine 不获取 `p.mu` 所以安全，但结构脆弱——未来新增 goroutine 若需获取锁会死锁。 | 将 `p.wg.Wait()` 移到 `p.mu.Unlock()` 之后（先 unlock 再 wait）。 |
| N-7 | `plugin/persist/persist.go` | 恢复后 reservoir 为空，percentile 查询返回 0 直到新样本到来。 | 可接受（MVP），在 README 已知限制中注明。 |
| N-8 | `plugin/management_handler.go:249` | plan 提到 config 端点应 redact 敏感字段，当前 `persist_path` 完整暴露。 | 对路径类字段考虑只显示文件名或做相对化处理。优先级低。 |

## 准入结论

**结论**：`条件准入`

**说明**：B-1（Snapshot 数据竞争）是唯一的阻塞项，修复方案明确（值拷贝 Bucket），改动量小（~5 行）。修复后即可准入。建议修改项中 S-1（排序性能）和 S-2（handleUsage 无锁读取）建议同批处理，其余可在后续迭代中解决。
