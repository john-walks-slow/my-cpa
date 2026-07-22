# 260722-stats-plugin · Summary

## 背景

为 CLIProxyAPI 实现模型速度统计插件，采集 TTFT / 总耗时 / token / 速率，按 (provider, model, alias, auth_id) × 时间窗口聚合，通过 `/v0/management/stats/*` 暴露查询 API。

## 实施结果

### 产出

- 完整可编译的插件源码（`plugin/` 目录，`package main`，`-buildmode=c-shared`）
- 单元测试通过（`-race` 干净）
- 跨平台构建脚本（`cmd/build/main.go`）
- Windows DLL 已验证编译成功

### 架构

```
HandleUsage → channel(1024) → Aggregator.ingestLoop → buckets[window][seriesKey]*Bucket
                                                        ↓ ticker
                                                   PersistWorker → JSON snapshot
HandleManagement → 读 Snapshot() → JSON response
```

### 模块

| 包 | 职责 |
|---|---|
| `plugin/` (main) | C ABI 入口、RPC 路由、usage/management handler、lifecycle |
| `plugin/aggregator` | 内存聚合：bucket、reservoir 分位数、retention、LRU 淘汰 |
| `plugin/persist` | JSON snapshot 原子写 + 启动恢复 |
| `plugin/config` | YAML 配置解析 + 默认值 |

### 关键决策

1. **package main**（非 plan 中的 `statsplugin`）：Go `-buildmode=c-shared` 要求 main 包
2. **同步 ingest 用于测试**：`IngestDirect` 方法绕过 channel，persist 测试无需等待
3. **SplitSeriesKey 放在 aggregator 包**：management handler 和测试共用
4. **SDK 版本 v7.2.93**：`go mod tidy` 自动解析的最新 release

### 与计划的偏差

- plan §3.1 建议 `package statsplugin`，实际必须用 `package main`（Go 工具链限制）
- plan 中的 `plugin/docs/README.md` 合并到项目根 `README.md`
- 未实现 `from`/`to` 时间范围过滤（series 查询按 window 返回当前桶，MVP 足够）

## 测试覆盖

- 聚合求和正确性
- 窗口 Truncate 对齐
- P50/P95 分位数（确定性小样本）
- cardinality_limit LRU 淘汰
- retention 过期清除
- 流式速率计算（streaming / non-streaming）
- 并发 ingest（10 goroutine × 100 samples，race 干净）
- persist 原子写 + 恢复 + 损坏文件 + 过期 snapshot
- config 合法/非法/负值回退
