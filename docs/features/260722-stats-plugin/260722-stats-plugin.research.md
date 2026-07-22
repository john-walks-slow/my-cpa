# 260722-stats-plugin · Research

> 调研结论：CLIProxyAPI 插件 ABI、可用能力、数据采集时序。用于设计「模型速度统计插件」。

仓库基础信息（基线）：

- 仓库：`github.com/router-for-me/CLIProxyAPI`（v7 模块路径）
- 插件机制：**C ABI 动态库（.so / .dylib / .dll）+ 进程内调用**
- 插件通过 Go `cgo` 暴露 `cliproxy_plugin_init`，由主程序以 dlopen 方式加载
- 主程序 ↔ 插件通信方式：**单一 `call(method, request) → response` RPC**，方法名 + base64 JSON body，envelope 协议
- 文档站：`https://help.router-for.me/plugin/`
- 入口参考示例：`examples/plugin/{usage,management-api,codex-service-tier,...}`

---

## 1. 插件加载与注册流程

### 1.1 部署形态

- 产物：C 共享库 `go build -buildmode=c-shared`
- 文件路径约定：主程序扫描 `plugins/<GOOS>/<GOARCH>/<id>.{so,dylib,dll}` 和 `plugins/`
- 安装方式：放进 plugins 目录后通过 `config.yaml` 的 `plugins.configs.<id>.enabled: true` 启用
- 配置文件 `config.yaml` 示例：

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    my-stats-plugin:
      enabled: true
      priority: 10
      retention_minutes: 60     # 自定义配置项，作为 ConfigYAML 传给插件
      persist_path: "stats.db"   # 可选
```

### 1.2 入口符号

```c
int cliproxy_plugin_init(const cliproxy_host_api* host, cliproxy_plugin_api* plugin);
```

Go 侧：

```go
//export cliproxy_plugin_init
func cliproxy_plugin_init(_ *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
    plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
    plugin.call        = C.cliproxy_plugin_call_fn(C.MyPluginCall)
    plugin.free_buffer = C.cliproxy_plugin_free_fn(C.MyPluginFree)
    plugin.shutdown    = C.cliproxy_plugin_shutdown_fn(C.MyPluginShutdown)
    return 0
}
```

调用约定参考 `examples/plugin/codex-service-tier/go/main.go`、`examples/plugin/jshandler/abi.go`。

### 1.3 RPC 方法清单（`sdk/pluginabi`）

| 方法名 | 用途 | 我们的插件是否需要 |
| --- | --- | --- |
| `plugin.register` / `plugin.reconfigure` | 注册 / 热重载（带 ConfigYAML） | 是 |
| `request.intercept_before` | 在鉴权前改写请求 | 否 |
| `request.intercept_after` | 在鉴权后改写请求 | 否 |
| `request.normalize` | 改写 canonical 请求体 | 否 |
| `response.intercept_after` | 改写非流响应体 | 否 |
| `response.intercept_stream_chunk` | 改写流式 chunk | **否（见 §4，关键洞察）** |
| `usage.handle` | 接收完成请求的使用记录（TTFT/latency/token） | **是（这就是我们的数据源）** |
| `management.register` / `management.handle` | 注册 plugin-owned `/v0/management/*` 路由 | **是（用于查询 API）** |
| `host.model.execute` 等 | 插件再发起内嵌调用 | 否 |

> 来源：`help.router-for.me/plugin/request-interceptor`、`sdk/pluginabi`、examples/plugin/{usage,management-api}。

---

## 2. 可用能力（`sdk/pluginapi/types.go`）

> 完整类型定义见 `4c4606fe-7ef9-46e7-9390-7f821e0087ba.txt`。我们关心的子集：

### 2.1 `UsagePlugin`（首选用作数据源）

```go
type UsagePlugin interface {
    HandleUsage(context.Context, UsageRecord)
}

type UsageRecord struct {
    Provider, ExecutorType, Model, Alias string
    APIKey, AuthID, AuthIndex, AuthType string
    Source                              string
    ReasoningEffort, ServiceTier         string
    RequestedAt                         time.Time
    Latency                             time.Duration
    TTFT                                time.Duration
    Failed                              bool
    Failure                             UsageFailure
    Detail                              UsageDetail
    ResponseHeaders                     http.Header
}

type UsageDetail struct {
    InputTokens, OutputTokens, ReasoningTokens int64
    CachedTokens, CacheReadTokens, CacheCreationTokens int64
    TotalTokens int64
}
```

来源：插件定义 `pluginapi.UsageRecord`；运行期实际产出物为 `sdk/cliproxy/usage.Record`（结构同形；host 在 SDK 边界做 envelope 转换）。

### 2.2 `ManagementAPI`（用作查询 API 通道）

```go
type ManagementAPI interface {
    RegisterManagement(context.Context, ManagementRegistrationRequest) (ManagementRegistrationResponse, error)
}

type ManagementRegistrationRequest struct {
    Plugin   Metadata
    BasePath string  // = "/v0/management"
}

type ManagementRegistrationResponse struct {
    Routes []ManagementRoute
}

type ManagementRoute struct {
    Method      string  // GET/POST
    Path        string  // 在 /v0/management/ 下绝对路径
    Menu        string  // 可选，UI 菜单
    Description string
    Handler     ManagementHandler
}

type ManagementHandler interface {
    HandleManagement(context.Context, ManagementRequest) (ManagementResponse, error)
}
```

- 路由前缀由 host 在注册时固定为 `/v0/management/`
- 鉴权要求：host 的 management middleware（同 `/v0/management/api-keys` 的 key/password）
- 我们注册到该前缀下的路由 **必须** 是相对路径，例如：`/stats/overview`、`/stats/by-model`
- 数据流：每次 host 收到匹配的请求 → 走 host middleware → `management.handle` 委托到我们插件 `HandleManagement` → 返回 `ManagementResponse{StatusCode, Headers, Body}`

### 2.3 元数据 / 配置

```go
type Metadata struct {
    Name, Version, Author, GitHubRepository, Logo string
    ConfigFields []ConfigField   // 暴露给管理 UI，用于自动生成配置表单
}

type ConfigField struct {
    Name        string
    Type        ConfigFieldType  // boolean/string/integer/number/enum/array/object
    EnumValues  []string
    Description string
}
```

- 接收在 `plugin.register` 请求中（envelope body 是 `abiLifecycleRequest{ConfigYAML, PluginDir}`）
- 重新配置走 `plugin.reconfigure`

---

## 3. 关键发现：可以直接用 `UsagePlugin`，无需拦截器

**主程序本身在每个请求结束后调用 `usage.PublishRecord`，并把记录派发给所有注册的 usage 插件**（参见 `internal/pluginhost/adapters.go` 的 `RegisterUsagePlugins` / `HandleUsage`）。

这意味着：

| 用户原始要求 | 采集方式 | 备注 |
| --- | --- | --- |
| 首 token 延迟 TTFT | `record.TTFT`（已由 host 计算） | 不需要 stream chunk interceptor |
| 总响应时间 | `record.Latency` | 同上 |
| 流式输出速率 tokens/sec | 在聚合时由 `record.OutputTokens` / (`record.Latency - record.TTFT`) 计算 | TTFT==0 表示非流，速率 = Total / Latency |
| 输入/输出 token 数 | `record.Detail.InputTokens` / `OutputTokens` | host 已经做了多 provider 归一 |
| 模型维度 | `record.Model` / `record.Alias` | 两个都给，alias 是用户态可见 |
| 账号维度 | `record.AuthID` / `record.AuthIndex` / `record.AuthType` | OAuth / API key 都能区分 |
| 失败统计 | `record.Failed` + `record.Failure.StatusCode` | 可分类统计 |

> **结论**：采用 `UsagePlugin + ManagementAPI` 两个 capability，**不启用**任何 request/response interceptor。避免拦截器链带来的性能开销和「重写语义」歧义。

---

## 4. 主程序 ↔ 插件的 envelope 协议

每个 `call(method, request)` 的 body 是 JSON envelope（至少在 Go/Rust 实现中是 base64 字符串包裹的 JSON 子对象）：

```json
// 请求 envelope
{
  "id": "uuid",
  "method": "plugin.register",
  "payload": "<base64(json body)>"
}

// plugin.register 的 payload 解码后：
{
  "ConfigYAML": "<YAML 字串>",
  "PluginDir": "<plugin 安装目录>"
}

// 响应 envelope（成功）
{
  "id": "uuid",
  "ok": true,
  "payload": "<base64(json)>"
}

// 响应 envelope（失败，2xx 由 host 解析）
{
  "id": "uuid",
  "ok": false,
  "error": {"code": "...", "message": "..."}
}
```

> 参考实现：`examples/plugin/codex-service-tier/go/main.go` 的 `okEnvelope`/`errorEnvelope`、`jsHandlerPlugin` 的 `handleJSHandlerRegister`。
>
> 我们采用 Go 实现，直接抄 `codex-service-tier` 模板即可。

---

## 5. 嵌入 host callbacks 的能力（可选）

host 提供反向调用函数给插件（用于插件调起 host 的能力）。当前需求不强，先列在心智清单：

- `host.model.*` 发起嵌套模型调用
- `host.http.*` 走 host 的传输层
- `host.auth_files.*` 读/写 auth 文件
- `host.credentials.*`

我们目前的需求（采集 + 查询）**不需要**调用 host callbacks。

---

## 6. 选型决策表

| 维度 | 候选 | 决定 | 理由 |
| --- | --- | --- | --- |
| 语言 | Go / Rust / C | **Go** | 已有 SDK 是 Go；与 CLIProxyAPI 同步升级最简单；作者维护能力 |
| 能力集 | UsagePlugin + ManagementAPI | **是** | 满足需求且最小依赖 |
| 数据源 | interceptor 自己测 vs UsagePlugin | **UsagePlugin** | 已有 TTFT/Latency/Tokens；零侵入；host 已聚合 |
| 时序粒度 | 流式 chunk vs 完成记录 | **完成记录** | chunk 调用 N 次/请求，完成记录 1 次/请求，开销小一个量级 |
| 持久化 | 仅内存 vs 内存+文件 | **内存 + 可选 BoltDB/sq** | 兼容性/复杂度问题；先默认内存，配置项开关落盘 |
| 时间窗口聚合 | 滑动 1m/5m/15m/1h | **可配置**（默认 1m/5m/15m/1h/24h） | 业界标准 metrics 时序 |
| 鉴权 | management key / 公开 | **沿用 host management key** | 与 `/v0/management/*` 同等待遇；user 一致 |

---

## 7. 风险与对照参考

- **`UsagePlugin` 字段在不同 SDK 版本可能新增字段**。本插件只读取 `Provider/Model/Alias/AuthID/Detail/Latency/TTFT/Failed/Failure`，新字段忽略即可，向前兼容。
- **`management.handle` 返回的 Body 必须是 raw bytes**，host 不会再做任何编码。文档站明确「不要在 HTML 里渲染 secrets」。
- **插件 panic 由 host 兜底**：`safeInvoke` 会 recover 后记 `log.Errorf`（usage 路径）。我们的 `HandleUsage` 应自己防御 panic。
- **ABI Version 不匹配**：当前 Go 示例取 `pluginabi.ABIVersion`，主程序也会检查。出现 mismatch 时 host 会拒绝加载并在 log 报错。
- **示例参考**：
  - `examples/plugin/usage/go/main.go` — 最简洁的 UsagePlugin 模板
  - `examples/plugin/management-api/go/main.go` — 最简洁的 ManagementAPI 模板
  - `examples/plugin/codex-service-tier/go/main.go` — Go-template 完整骨架（envelope、shutdown、free_buffer 都齐）

---

## 8. 结论（用于 plan 的输入）

1. 项目形态：独立 Go module，编译为 `-buildmode=c-shared`
2. 依赖：仅 `github.com/router-for-me/CLIProxyAPI/v7/sdk/{pluginabi,pluginapi}` 两个包
3. 能力：`usage_plugin: true` + `management_api: true`（同时声明）
4. 数据流：`HandleUsage` 入队 → 内存聚合（按 Model × AuthID × 时间窗口）→ 可选落盘 → `HandleManagement` 返回 JSON
5. 元数据 `Name = "my-stats-plugin"`、`Author` 用户名、`GitHubRepository` 用户仓库
6. `ConfigFields`：暴露 `enabled/retention/persist_path/persist_interval` 等可选项
7. 构建产物命名：`stats-plugin.so`（Linux）/ `stats-plugin.dylib`（macOS）/ `stats-plugin.dll`（Windows）
8. 部署：`plugins/<GOOS>/<GOARCH>/stats-plugin.<ext>`，host 扫描时自动发现
9. **不需要**：interceptor、host callbacks、文件 IO 之外的外部依赖

