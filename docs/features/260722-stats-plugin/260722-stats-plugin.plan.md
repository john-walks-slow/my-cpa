# 260722-stats-plugin · Plan

> 模型速度统计插件：为 CLIProxyAPI 采集 TTFT / 总耗时 / token / 速率，按 model × auth 聚合并通过 management API 暴露。
> 关联调研：`260722-stats-plugin.research.md`

---

## 1. 目标与范围

### 1.1 用户故事

> 作为 CLIProxyAPI 运维，我需要在 `/v0/management/stats/...` 下查询每个 (model × auth) 时间窗口的：请求计数、成功率、平均 TTFT、平均延迟、平均流式输出速率、输入/输出/缓存 token 用量。

### 1.2 范围内

- 数据采集：仅依赖 host 的 `UsagePlugin` 能力
- 聚合：内存，按 (provider, model, alias, auth_id) × 时间桶
- 时间窗口：1m / 5m / 15m / 1h / 24h（固定集合，按需暴露）
- 持久化：可选 JSON snapshot 文件（默认关闭）
- 查询 API：注册到 `/v0/management/stats/...`
- 配置：`ConfigYAML` 接收 retention、persist_path、persist_interval、enabled、cardinality_limit
- 部署产物：`-buildmode=c-shared` 输出的 `.so` / `.dylib` / `.dll`

### 1.3 范围外（明确不做）

- 拦截器链（不重写请求/响应 → 零业务风险）
- 调用 host callbacks（无需求）
- 与外部 metrics 后端（Prometheus OTLP 等）对接
- 写时序数据库（仅做可选 JSON snapshot）
- 用户态 UI / 资源页（先纯 JSON API）
- 跨插件事件订阅

### 1.4 已确认的数据缺口

> 调研发现 `internal/runtime/executor/...usage_helpers.go` 的 `publishWithOutcome` 在「成功且全部 token 字段为 0」时**跳过** `PublishRecord`。这意味着：
>
> - 大多数正常请求都会被采集（provider 返回 token 信息）
> - 不返回 usage 的成功响应不会被采集（如部分 Gemini 文本端点的零 token case）
> - 失败请求**始终**被采集
>
> 该限制在后续阶段通过 `ResponseInterceptor`（只读 hit counter）补齐时再做。当前范围接受此限制并在 README 中说明。

---

## 2. 架构总览

```
┌─────────────────────────────────────────────┐
│ CLIProxyAPI Host Process                    │
│                                             │
│  usage.Manager  ──── record ───▶  UsagePlugin.Adapter
│                                  │
│                                  ▼
│                         (C ABI via dlsym)
│                                  │
│                                  ▼
│  ┌────────────────────────────────────────┐ │
│  │ my-cpa stats-plugin (.so)              │ │
│  │                                        │ │
│  │  HandleUsage ──▶ channel ──▶ Aggregator│ │
│  │                          │             │ │
│  │                          ▼             │ │
│  │                      [Buckets]          │ │
│  │                          │              │ │
│  │                          ▼ periodic     │ │
│  │                     PersistWorker       │ │
│  │                          │              │ │
│  │                          ▼ on-demand    │ │
│  │  HandleManagement ◀──── HTTP GET        │ │
│  └────────────────────────────────────────┘ │
│                                             │
│  (host )  /v0/management/stats/...           │
│       │                                     │
│       ▼                                     │
│  middleware(management key) ─▶ adaptHandle   │
└─────────────────────────────────────────────┘
```

### 2.1 数据流时序

1. host 完成一次 provider 调用 → `usage.PublishRecord`
2. host 派发到 `rpcUsageHandle` → C ABI → 我们的 `PluginCall(method="usage.handle", payload)`
3. 我们 `json.Unmarshal` → `aggregate.Record` → 丢进 buffered chan（容量 1024）
4. 单独 goroutine `Aggregator.run` 从 chan 取出，按 (provider, model, alias, auth_id, bucket_minute) 累加
5. 同时启动 `RetentionWorker` 每 1 分钟扫描，丢弃超出 retention 的桶
6. 可选 `PersistWorker` 每 N 秒落盘（snapshot 整个聚合状态 + WAL-style 增量）
7. HTTP GET `/v0/management/stats/...` → host 调 `management.handle` → 我们读内存 + 拼 JSON 返回

### 2.2 关键不变量

- `HandleUsage` **不阻塞** host 的派发队列；超过 channel 容量时丢弃并在日志计数
- 内存聚合上限由 `cardinality_limit` 限制（默认 50,000 行），超出 LRU
- 所有 goroutine 在 `cliproxy_plugin_shutdown` 时通过 `context.Cancel` 退出
- `HandleManagement` 不写状态，纯读

---

## 3. 模块划分

```
my-cpa/                                  ← 项目根
├── go.mod                               ← 模块名: github.com/John/my-cpa
├── README.md
├── .gitignore                           ← bin/, *.so, *.dylib, *.dll, stats.data
├── cmd/
│   └── build/
│       └── main.go                      ← build 脚本: 跨平台 c-shared build
├── plugin/                              ← 唯一 Go 包 (包名: statsplugin)
│   ├── plugin.go                        ← cliproxy_plugin_init / entrypoint
│   ├── envelope.go                      ← okEnvelope / errorEnvelope / parseEnvelope
│   ├── lifecycle.go                     ← plugin.register / plugin.reconfigure / shutdown
│   ├── capabilities.go                  ← Capabilities() 方法注册 usage+management
│   ├── usage_handler.go                 ← HandleUsage 入 channel
│   ├── management_handler.go            ← RegisterManagement + 路由 + HandleManagement
│   ├── aggregator/
│   │   ├── aggregator.go                ← 聚合主循环 + 写状态
│   │   ├── bucket.go                    ← 1m / 5m / 15m / 1h / 24h bucket
│   │   ├── record.go                    ← 内部样本（去重 + 求和 + Welford）
│   │   └── retention.go                 ← 定时淘汰
│   ├── persist/
│   │   ├── persist.go                   ← SnapshotWriter/Reader (atomic rename)
│   │   └── persist_test.go
│   ├── config/
│   │   └── config.go                    ← ConfigYAML 解析 + 默认值
│   └── docs/
│       ├── README.md                    ← 用户文档（安装 / 配置 / API）
│       └── config.example.yaml
└── docs/features/260722-stats-plugin/
    ├── 260722-stats-plugin.research.md  ← 已完成
    ├── 260722-stats-plugin.plan.md     ← 本文
    ├── 260722-stats-plugin.validation.md ← 用户实机验证用
    └── 260722-stats-plugin.review.md   ← 实施完成后用
```

### 3.1 包结构注意

- **package 名为 `statsplugin`**（`package statsplugin`），不是 `main`、不是 `plugin`（避免与 SDK 名称冲突）
- `plugin/` 下子目录 `aggregator/`、`persist/` 是 **同包**（子文件目录仅用于组织代码）
- 不拆 `internal/` —— c-shared 插件的 `main` 包会与 go-build 模式冲突，统一单包最简

### 3.2 文件职责

| 文件 | 职责 | 关键 export |
| --- | --- | --- |
| `plugin.go` | `//export cliproxy_plugin_init`，填充 `plugin_api` 结构 | (无，纯 C export) |
| `envelope.go` | RPC envelope 编解码 | `parseRequest`, `okEnvelope`, `errorEnvelope` |
| `lifecycle.go` | `plugin.register` / `plugin.reconfigure` / `shutdown` 方法路由 | `handleLifecycle` |
| `capabilities.go` | 返回 `pluginapi.Plugin{Capabilities: ...}` | `pluginRegistration()` |
| `usage_handler.go` | `usage.handle` → enqueue sample | `UsageRecord → sample` 转换 |
| `management_handler.go` | `management.register` 与各路由 | `handleStats*` |
| `aggregator/aggregator.go` | 接收 sample、维护 buckets、提供快照与查询 | `Aggregator`, `Snapshot` |
| `aggregator/bucket.go` | 固定集合时间窗口 | `bucketKey`, `rollUpToAll` |
| `aggregator/record.go` | 单 (provider, model, alias, auth_id) 上的在线统计 | `Sample`, `Welford` |
| `aggregator/retention.go` | 扫描+丢弃过期桶 | `Retention` |
| `persist/persist.go` | 原子 snapshot + 启动时恢复 | `SnapshotStore` |
| `config/config.go` | YAML → 配置 | `Load(raw []byte) (Config, error)` |

---

## 4. 关键接口契约

### 4.1 `plugin.init`（main 入口）

```go
//export cliproxy_plugin_init
func cliproxy_plugin_init(_ *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
    plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
    plugin.call        = C.cliproxy_plugin_call_fn(C.statsPluginCall)
    plugin.free_buffer = C.cliproxy_plugin_free_fn(C.statsPluginFree)
    plugin.shutdown    = C.cliproxy_plugin_shutdown_fn(C.statsPluginShutdown)
    return 0
}
```

> 模板抄自 `examples/plugin/codex-service-tier/go/main.go`。

### 4.2 Capabilities 注册

```go
func pluginRegistration() registration {
    return registration{
        SchemaVersion: pluginabi.SchemaVersion,
        Metadata: pluginapi.Metadata{
            Name:    "my-cpa-stats-plugin",
            Version: "0.1.0",
            Author:  "<username>",
            GitHubRepository: "<repo URL>",
            Logo:    "<logo URL>",
            ConfigFields: []pluginapi.ConfigField{
                {Name: "enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Enable the stats aggregator."},
                {Name: "retention_minutes", Type: pluginapi.ConfigFieldTypeInteger, Description: "Retention window in minutes (default 1440)."},
                {Name: "persist_path", Type: pluginapi.ConfigFieldTypeString, Description: "Snapshot file path. Empty means no persistence."},
                {Name: "persist_interval_sec", Type: pluginapi.ConfigFieldTypeInteger, Description: "Snapshot interval seconds (default 30)."},
                {Name: "cardinality_limit", Type: pluginapi.ConfigFieldTypeInteger, Description: "Max unique (provider,model,alias,auth_id) series kept (default 50000)."},
            },
        },
        Capabilities: registrationCapability{
            UsagePlugin:   true,
            ManagementAPI: true,
        },
    }
}
```

### 4.3 Usage handle

```go
func (a *aggregatorAdapter) HandleUsage(ctx context.Context, rec pluginapi.UsageRecord) {
    a.queue <- aggregator.Sample{
        Provider:        rec.Provider,
        Model:           rec.Model,
        Alias:           rec.Alias,
        AuthID:          identityKey(rec.APIKey, rec.AuthID, rec.AuthIndex, rec.AuthType),
        Source:          rec.Source,
        RequestedAt:     rec.RequestedAt,
        Latency:         rec.Latency,
        TTFT:            rec.TTFT,
        Failed:          rec.Failed,
        StatusCode:      rec.Failure.StatusCode,
        InputTokens:     rec.Detail.InputTokens,
        OutputTokens:    rec.Detail.OutputTokens,
        ReasoningTokens: rec.Detail.ReasoningTokens,
        CachedTokens:    rec.Detail.CachedTokens,
    }
}
```

> `identityKey` 把 API key / OAuth 都规整为可读字符串。空值退化为 `<source>:<provider>`。

### 4.4 Management routes

| Method | Path | 描述 |
| --- | --- | --- |
| GET | `/stats/overview` | 顶层 total / success / failed / p50/p95 latency + 模型/账号条目数 |
| GET | `/stats/series` | 查询参数：`window=1m\|5m\|15m\|1h\|24h`、`model=<id>`、`auth=<id>`、`from=<rfc3339>`、`to=<rfc3339>`、`limit=<n>`。返回时间序列 |
| GET | `/stats/by-model` | 给定 window，列出每个 (provider,model) 行的聚合 |
| GET | `/stats/by-auth` | 给定 window，列出每个 (provider,auth_id) 行的聚合 |
| GET | `/stats/keys` | 列出已知的 (provider,model,alias,auth_id) 元数据 |
| POST | `/stats/reset` | 清空内存聚合（保留 auth_key 注册，慎用） |
| GET | `/stats/config` | 返回当前生效配置（部分敏感字段 redact） |

所有路由：
- 全部 `Description`、`Menu` 标注好（用于将来 UI）
- 返回 JSON（`Content-Type: application/json`）
- 错误：JSON `{"error":"..."}`，4xx/5xx

### 4.5 ConfigYAML 示例

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    my-cpa-stats-plugin:
      enabled: true
      priority: 100
      retention_minutes: 1440          # 24h
      persist_path: "stats.json"        # 留空则不落盘
      persist_interval_sec: 30
      cardinality_limit: 50000
```

---

## 5. 数据模型

### 5.1 Sample（单次请求归约）

```go
type Sample struct {
    Provider, Model, Alias, AuthID, Source string
    RequestedAt time.Time
    Latency, TTFT time.Duration
    Failed bool
    StatusCode int
    InputTokens, OutputTokens, ReasoningTokens, CachedTokens int64
}
```

> 在 Aggregator 中按 `requestedAt` 落到 timestamp 对应的桶。

### 5.2 BucketKey

```
key = "{provider}|{model}|{alias}|{auth_id}"
```

> 使用 `escape '|'` 防止值中含竖线（虽然现实里几乎不会）。

### 5.3 Bucket（一个 (key, window) 上的聚合）

```go
type Bucket struct {
    Window time.Duration             // 1m/5m/15m/1h/24h
    Start  time.Time                 // 桶起点（按 window 向上对齐）

    Count        uint64               // 请求总数
    Failed       uint64
    SumLatency   time.Duration
    SumTTFT      time.Duration
    SumOutput    int64                // tokens
    SumInput     int64
    SumReasoning int64
    SumCached    int64

    // 分位数（t-digest 二选一：先实现 P50 + P95 + P99 reservoir；后续看是否升级 t-digest）
    LatencyP50 time.Duration
    LatencyP95 time.Duration
    LatencyP99 time.Duration

    // 流式速率（不是端到端的）
    StreamRateSum   float64            // tokens/sec
    StreamRateCount uint64

    LastSampleAt time.Time
}
```

> 分位数实现选择：先用 reservoir sampling（容量 1024 即可），简单稳定。

### 5.4 内存结构

```
map[Window]map[BucketKey]*Bucket
   │
   └─ 5 个 window，key 空间独立
```

聚合主循环伪码：

```go
func (a *Aggregator) ingest(s Sample) {
    now := s.RequestedAt
    for _, w := range allWindows {
        bucketStart := now.Truncate(w)
        key := bucketKey(s)
        b := a.buckets[w].getOrCreate(key, bucketStart)
        b.accumulate(s)
    }
    // 触发 retention（懒触发，每 N 次一次）
}
```

---

## 6. 持久化（可选）

### 6.1 存储格式

单文件 JSON snapshot：

```json
{
  "schema_version": 1,
  "saved_at": "2026-07-22T00:00:00Z",
  "windows": {
    "1m":  { "<key>": { "start": "...", "count": ..., "sum_latency_ms": ... } },
    "5m":  { ... },
    "1h":  { ... }
  }
}
```

### 6.2 原子写

1. 写 `stats.json.tmp`
2. `fsync`
3. `rename(stats.json.tmp, stats.json)`（POSIX 保证原子）

### 6.3 启动恢复

1. 读 `stats.json`，校验 `schema_version`，剔除过期桶
2. 与增量：MVP 阶段不支持 WAL。仅恢复 snapshot。如果 snapshot 比 `retention_minutes` 老，整个清空从零开始（更可预测）

### 6.4 何时写

- 启动时主动恢复
- 运行期由 ticker（`persist_interval_sec`）写
- `cliproxy_plugin_shutdown` 最后一次写

### 6.5 错误处理

- 不可写的路径：记 log，禁用持久化但插件继续运行
- 不可读的 snapshot：记 log，跳过恢复

---

## 7. 并发模型

| 组件 | Goroutine | 同步 |
| --- | --- | --- |
| PluginCall（C 入口） | 临时（host 调用） → 短进入 channel | chan 缓冲；满则丢 + drop counter |
| Aggregator.run | 1 | 独占写 buckets |
| RetentionWorker | 1 | 借助 Aggregator mutex |
| PersistWorker | 1 | ticker |
| Management HTTP 处理（host 端） | 临时（host） | RLock 读 |

`sync.RWMutex` 保护整个 `buckets` map；分位数 reservoir 与计数器在 Bucket 内部独立锁（粒度小）。简单优先。

---

## 8. 配置项 & 默认值

| Key | 类型 | 默认 | 含义 |
| --- | --- | --- | --- |
| `enabled` | bool | true | 整体开关（关闭时 HandleUsage 直接 return） |
| `retention_minutes` | int | 1440 (24h) | 桶最长保留时间 |
| `persist_path` | string | "" | 快照文件路径 |
| `persist_interval_sec` | int | 30 | 持久化周期 |
| `cardinality_limit` | int | 50000 | 最大活跃 series；超限 LRU 淘汰 |

---

## 9. 构建产物与安装

### 9.1 构建命令

`cmd/build/main.go` 是一个 build orchestrator：

```go
//go:build ignore

package main

// 跨平台 c-shared build：
//   mkdir -p bin/<GOOS>/<GOARCH>
//   go build -buildmode=c-shared -o bin/<GOOS>/<GOARCH>/my-cpa-stats-plugin<ext> .
// ext: linux=.so, darwin=.dylib, windows=.dll
```

实际执行通过 shell：

```bash
mkdir -p bin/$(go env GOOS)/$(go env GOARCH)
GOOS=linux  go build -buildmode=c-shared -o bin/linux/amd64/my-cpa-stats-plugin.so  .
GOOS=darwin go build -buildmode=c-shared -o bin/darwin/arm64/my-cpa-stats-plugin.dylib .
GOOS=windows go build -buildmode=c-shared -o bin/windows/amd64/my-cpa-stats-plugin.dll .
rm -f bin/*/my-cpa-stats-plugin.h
```

> 抄 `examples/plugin/simple/README.md` 的习惯。

### 9.2 安装到 host

```
my-cpa-stats-plugin/
├── bin/
│   ├── linux/amd64/my-cpa-stats-plugin.so
│   ├── darwin/arm64/my-cpa-stats-plugin.dylib
│   └── windows/amd64/my-cpa-stats-plugin.dll
├── docs/...
└── README.md
```

放进 CLIProxyAPI 的 `plugins/<GOOS>/<GOARCH>/` 目录，host 启动时会自动加载。

---

## 10. 测试要点

### 10.1 单元测试

- `aggregator`：
  - 多次 ingest 的求和正确性
  - 窗口向上取整（`Truncate`）
  - P50/P95 在已知样本上正确（用 deterministic 小样本验证）
  - cardinality_limit 触发后旧 series 被淘汰
  - 并发：race detector 干净
- `persist`：原子写 + 启动恢复；损坏文件不 panic
- `config`：合法 + 非法 YAML 解析
- `envelope`：编解码回环

### 10.2 集成测试（mock 不上 host，改为契约测试）

- 对照 `sdk/pluginapi/types.go` 的契约签名编译，确保 `UsagePlugin`/`ManagementAPI` 实现匹配
- 对照 `sdk/cliproxy/usage.Record` 字段对每个 Sample 字段（编译期类型保证）

### 10.3 端到端验证（人工）

- 启动 CLIProxyAPI dev 模式（本地版本，dev branch）
- 加载本插件
- 发起若干 OpenAI/Codex/Gemini 测试请求（混合流/非流/失败）
- 通过 `curl -H "Authorization: Bearer <key>" http://localhost:8317/v0/management/stats/series?window=1m` 验证
- 详细验证清单见 `260722-stats-plugin.validation.md`

---

## 11. 已知风险与备选

| 风险 | 备选方案 |
| --- | --- |
| 大基数（数十万 model×auth）压内存 | 加采样 + LRU（已设计） |
| Persist 文件被删/损坏 | 检测 + 跳过恢复 + 记日志 |
| Welford/t-digest 数值稳定性 | 先用 reservoir sampling（小容量），必要时换 |
| host 的 `UsageRecord` 字段新增 | 仅读我们需要的字段，新字段忽略 |
| 多插件共享同一个 model/auth 维度 | 加 `plugin` 标识到 key（`{plugin_id}|{provider}|...`） |
| Go c-shared 在某些版本的 dlopen 兼容性问题 | 与 host 的 Go 版本对齐（`go.mod` 中声明与 `router-for-me/CLIProxyAPI` 同样 `go 1.XX`） |

---

## 12. 实施任务拆分（实施时使用）

> 不是文档，是给后续 `/workflow-implement-review` 用的检查清单。

1. 创建项目骨架（go.mod、目录、.gitignore、README）
2. `plugin/envelope.go` — RPC 编解码
3. `plugin/config/config.go` — YAML 配置 & 默认
4. `plugin/aggregator/record.go` + `bucket.go` — 数据结构 + 聚合
5. `plugin/aggregator/aggregator.go` — 主循环
6. `plugin/aggregator/retention.go` — 过期淘汰
7. `plugin/persist/persist.go` — 快照写入/恢复
8. `plugin/usage_handler.go` + `plugin/management_handler.go` — 把 adapter 串起来
9. `plugin/capabilities.go` + `plugin/lifecycle.go` — 注册逻辑
10. `plugin/plugin.go` — C export + shutdown
11. `cmd/build/main.go` — 跨平台 build
12. 单元测试
13. 端到端验证（CLIProxyAPI dev 实例）

---

## 13. 对齐用户预期（待用户确认）

请用户核对以下决策是否符合预期，再进入实施阶段：

1. **采用 UsagePlugin + ManagementAPI，不启用拦截器** —— 主要决策
2. **不持久化或仅 JSON snapshot** —— 不引入 BoltDB/Timescale
3. **不提供 UI / 资源页** —— 纯 JSON API
4. **接受「零 token 成功请求不计数」的局限**
5. **保留时间默认 24h**（可配置）
6. **不调用 host callbacks**（我们没有嵌套调用需求）

如有任一不同意，请告知，我重新设计。
