# 260722-dashboard · Research

> 调研结论：图表库选型、CLIProxyAPI 静态资源方案、移动端/桌面端权衡、设计参考。
> 关联需求：在 stats 插件（`docs/features/260722-stats-plugin/`）基础上构建一个真正可用、专注趋势分析的内嵌 dashboard 页面。

---

## 0. 基线（来自 stats 插件）

- 仓库：`github.com/John/my-cpa`
- 模块：单 Go 包 `main`，`-buildmode=c-shared`，产物 `.so/.dylib/.dll`
- 已暴露端点：`GET /v0/management/stats/{overview,series,by-model,by-auth,keys,config}`、`POST /v0/management/stats/reset`
- 鉴权：沿用 host management key（`/v0/management/api-keys` 的 key/password）
- 数据窗口：1m / 5m / 15m / 1h / 24h，分位数 P50/P95（reservoir）

> ⚠️ **已知 bug**：因 `Menu: "Stats"` 副作用（见 §1.3），当前实际暴露路径是 `/v0/resource/plugins/my-cpa-stats-plugin/stats/...`，**不是 README 写的 `/v0/management/stats/...`**。本次 dashboard 工作同时修复。

## 1. 关键事实（CLIProxyAPI v7.2.93 host 行为）

> 直接从 `internal/pluginhost/management.go` 与 `sdk/pluginapi/types.go` 验证。

### 1.1 `ManagementRoute.Handler` 字段被 host 覆盖

`internal/pluginhost/rpc_client.go:532-540`：

```go
for _, route := range resp.Routes {
    route.Handler = a   // host 注入自己的 adapter
    routes = append(routes, route)
}
...
for _, route := range resp.Resources {
    route.Handler = a
    resources = append(resources, route)
}
```

**结论**：插件只需要提供 `Method/Path/Menu/Description`，handler 字段**只是 RPC 适配器占位**，host 端一律覆盖为 `a`（`rpcPluginAdapter`）。当前 stats 插件所有 `Handler: nil` 是无害的——只要 host 收到路由就由 `HandleManagement` 统一分发。所以**现有 routes 已能工作**（这是 review.md 没注意到的：所有现有 routes 实际是工作的）。

### 1.2 `ResourceRoute` 是 dashboard 的目标路径

`pluginapi.ManagementRegistrationResponse.Resources` 暴露在：

```
GET /v0/resource/plugins/<pluginID>/<path>
```

- **不走** management 鉴权（`ServeResourceHTTP` 在 `internal/pluginhost/management.go:286-327` 中直接 dispatch）
- 资源响应**不会**被 `htmlsanitize.JSONBodyIfLikely` 改写（只有 management 响应会被 XSS escape）
- 这是 HTML/JS/CSS 静态资源的**正确**挂载点
- 注册路径限制：`/v0/resource/plugins/<id>/<sub-path>`，不能含 `:`/`*`/`..`，不能含空白

### 1.3 Menu 字段的副作用（**重要：影响现有 stats 插件**）

`routeDeclaresLegacyMenuResource(method, item)`：GET 路由若 `Menu != ""`，**自动**转为 resource 路由（host 把 `ManagementRoute` 转 `ResourceRoute`），且 **continue** 跳过 management 路径。

**回看现有 stats 插件**（`plugin/management_handler.go:30-37`）：

```go
{Method: "GET", Path: "/stats/overview", Menu: "Stats", ...},   // ← Menu 非空
{Method: "GET", Path: "/stats/series",   Menu: "Stats", ...},
{Method: "GET", Path: "/stats/by-model", Menu: "Stats", ...},
{Method: "GET", Path: "/stats/by-auth",  Menu: "Stats", ...},
{Method: "GET", Path: "/stats/keys",     Menu: "Stats", ...},
{Method: "POST", Path: "/stats/reset", ...},                  // ← Menu 空，POST 不参与
{Method: "GET", Path: "/stats/config",   Menu: "Stats", ...},
```

**所有带 `Menu: "Stats"` 的 GET 路由被 host 转为 resource 路径**，实际暴露在：

```
GET /v0/resource/plugins/my-cpa-stats-plugin/stats/overview
GET /v0/resource/plugins/my-cpa-stats-plugin/stats/series
GET /v0/resource/plugins/my-cpa-stats-plugin/stats/by-model
GET /v0/resource/plugins/my-cpa-stats-plugin/stats/by-auth
GET /v0/resource/plugins/my-cpa-stats-plugin/stats/keys
GET /v0/resource/plugins/my-cpa-stats-plugin/stats/config
```

而不是 README 写的 `/v0/management/stats/...`。

副作用：

1. **不走 management 鉴权**：任何人能直接 GET `/v0/resource/plugins/.../stats/overview`，无需 management key
2. **JSON 响应被 XSS escape**：`ServeResourceHTTP` 没有走 `escapeManagementResponseBody`，但 host 的 `htmlsanitize.JSONBodyIfLikely` 实际只在 management 路径触发；resource 路径下 JSON 字符串保持原样。前端必须自己处理 XSS。
3. **JSON 路径与 README 描述不符**：用户按 README 测 `/v0/management/stats/overview` 会得到 404（management route 未注册）

> **本次 dashboard 工作包含修复**：移除所有 `/stats/*` GET 路由的 `Menu` 字段，使其注册为 management 路由（要求 management key），与 README 一致。`POST /stats/reset` 无 Menu，已正确。

### 1.4 `/v0/management` 走 management 鉴权

JSON API 仍走 `/v0/management/stats/*`（host `s.mgmt.Middleware()(c)` 校验 key），dashboard 的数据 fetch 必须带 `Authorization: Bearer <key>` 头。

### 1.5 Host 没有自动 gzip 中间件

`internal/api/server.go` 全局未挂 gzip middleware。Go `embed.FS` 也不做压缩。**plugin 端需要自己选择**：要么不压缩（接受 ~150 KB 总载荷），要么在 `RegisterManagement` 中预生成 `.gz` 通过 `Content-Encoding` 头输出。建议先不做 gzip（载荷足够小），留作后续优化点。

---

## 2. 图表库选型

### 2.1 矩阵（基于 uPlot 官方 bench + 实测基准）

| 维度 | **uPlot** | Chart.js | ECharts | 说明 |
|---|---|---|---|---|
| 压缩后体积（IIFE min + CSS） | **~37 KB** | ~80 KB | ~330 KB | uPlot 最小 |
| 冷启动渲染 50K 点 | **34 ms / 21 MB heap** | 38 ms / 29 MB | 55 ms / 17 MB | uPlot 完胜综合表现 |
| 长时间 streaming FPS | **稳定 60 FPS（10min+）** | 5min 25 FPS / 30min 8 FPS | 5min 20 FPS / 30min 6 FPS | Chart.js / ECharts 累积数组降速 |
| Time series 专用 | **是**（仅 time + axes + lines/area） | 通用 | 通用（功能多但本场景浪费） | — |
| 多 series / 图例 / 缩放 | 支持（hooks） | 支持 | 支持 | 都够用 |
| Tooltip | 需自实现（demo 30 行） | 内置 | 内置 | uPlot 需少量 JS |
| 触摸缩放 / 平移 | 需 plugin（demo 现成） | 内置 | 内置 | uPlot 略多代码 |
| 响应式尺寸 | 需 `ResizeObserver` + `setSize` | 内置 | 内置 | uPlot 不自动跟踪容器 |
| 零依赖 | **是** | 是 | 是（但 themes/locale 是独立文件） | 三者都无第三方 JS |
| License | MIT | MIT | Apache 2.0 | 商用均 OK |
| 与 c-shared Go 嵌入契合度 | **高**（单个 .min.js） | 中（多文件） | 低（themes + locale + zrender） | 单文件优先 |

数据来源：

- `github.com/leeoniya/uPlot` 官方 bench（50KB+、166K 点 cold start 25ms）
- `lightningchart.com/blog/best-chart-js-alternatives-in-2026/` 独立 23-library 基准

### 2.2 选型决定：**uPlot**

理由：

1. **单文件**（IIFE min 35 KB + CSS 1.8 KB），完美匹配 `embed.FS` 单包嵌入
2. **专为 time series 设计**，与我们场景天然匹配（指标就是 time series）
3. **canvas-based 内存效率高**，长期 streaming 不衰减（多 model 多 auth 同时显示数小时是常态）
4. **零运行时依赖**，不引入第三方 JS（host 上没有 npm/Node 资源）
5. 体积/性能差距大到不值得选 Chart.js/ECharts；功能差距（tooltip、touch、resize）通过官方 demo 30-50 行代码补齐

**对照考虑过的 ECharts 备选**：虽然企业 dashboard 功能丰富，但 1MB 体积 + 多文件 + streaming 衰减是 deal-breaker。

### 2.3 uPlot 必须自实现的薄薄一层

- Tooltip 插件（参考 `demos/tooltips.html`，约 50 行 JS）
- Pinch / 触摸缩放（参考 `demos/zoom-touch.html`，约 60 行 JS）
- ResizeObserver + `u.setSize`（10 行）
- 主题切换（亮/暗，跟随 `prefers-color-scheme`）

> 这些都属于"自然适配 uPlot"工作，不算选型风险。

---

## 3. 静态资源嵌入方案

### 3.1 候选方案

| 方案 | 描述 | 评价 |
|---|---|---|
| A. `//go:embed` + `http.FileServer(http.FS(sub))` | 标准库方案，把 `web/dist/*` 嵌入到二进制，通过 ResourceRoute 输出 | **推荐**：最简单；零依赖；reload 二进制即可更新 |
| B. 第三方 `spaserver` / `mizu` | 提供现成 SPA fallback + CSP header | 引入外部依赖；项目当前零外部依赖（除 SDK），引入理由不足 |
| C. base64 inline（数据 URL） | 把 HTML/JS/CSS 全部 base64 写进 Go | 不可读、不可调试；不推荐 |
| D. gzip 预生成 + `Content-Encoding` | embed 时读 `.gz`，按 Accept-Encoding 返回 | 当前载荷 < 150KB，过早优化；标记为 follow-up |

**决定 A**：用 `//go:embed` + `http.FS` + `http.FileServer`。

### 3.2 文件布局

```
plugin/
├── web/
│   └── dist/                     ← //go:embed 嵌入根
│       ├── index.html
│       ├── uPlot.iife.min.js     ← vendored，~35 KB
│       ├── uPlot.min.css         ← vendored，~1.8 KB
│       ├── app.js                ← 我的 dashboard JS，~20-40 KB
│       └── app.css               ← 我的 dashboard CSS，~5-10 KB
├── web/
│   └── embed.go                  ← //go:embed web/dist/* var webFS embed.FS
```

开发期约定：

- `web/dist/` 直接在仓库跟踪（vendored，文件小可接受）
- 不引入 npm/yarn 构建链；手写 JS
- 用 `cmd/build/main.go` 之前的工具复制 uPlot 发行包到 `web/dist/`（一次性；脚本可放进 README）

### 3.3 Resource route 注册

```go
func (p *pluginState) handleManagementRegister(raw []byte) ([]byte, error) {
    routes := []pluginapi.ManagementRoute{
        {Method: "GET", Path: "/stats/overview"},
        {Method: "GET", Path: "/stats/series"},
        // ... 不要 Menu，避免被 host 当 legacy resource route
    }
    resources := []pluginapi.ResourceRoute{
        {Path: "/", Menu: "Stats Dashboard", Description: "Visual dashboard.", Handler: dashboardHandler},
        {Path: "/assets/*", Menu: "", Description: "", Handler: dashboardAssetsHandler},
    }
    return okEnvelope(pluginapi.ManagementRegistrationResponse{Routes: routes, Resources: resources})
}
```

> **注意**：把现有 7 条 stats routes 的 `Menu` 字段移除，避免被 host 转成 resource 路由。

### 3.4 Host resource 路径规整

`normalizeResourceRoute` 验证：路径必须以 `/v0/resource/plugins/<pluginID>/` 开头；不能含 `:`/`*`/`..`/空白。

我们的子路径：

- `/` → `/v0/resource/plugins/my-cpa-stats-plugin/`
- `/assets/uPlot.iife.min.js` → `/v0/resource/plugins/my-cpa-stats-plugin/assets/uPlot.iife.min.js`

resource handler 内需要从 `req.Path` 提取子路径，查 `webFS` 返回内容。**路径白名单**：仅匹配 `index.html` 与 `assets/*` 子集；其他返回 404。

### 3.5 MIME & 缓存

- `index.html` → `Cache-Control: no-cache`（deploy 后立即生效）
- `uPlot.iife.min.js` / `app.js` → `Cache-Control: public, max-age=3600`（1h，软约束）
- `uPlot.min.css` / `app.css` → `Cache-Control: public, max-age=3600`
- 默认 MIME 用 `mime.TypeByExtension`（标准库）

---

## 4. 前端信息架构（IA）

### 4.1 页面布局（desktop ≥ 1024px）

```
┌─────────────────────────────────────────────────────────┐
│  Header  ┌─ title ─┐  ┌─ range [15m|1h|6h|24h|7d] ┐     │
│          └──────────┘  └─ auto-refresh [off|30s] ┘     │
├─────────────────────────────────────────────────────────┤
│  KPI Cards (4 块，2x2 grid)                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐ │
│  │ Requests │  │ P50 TTFT │  │ Avg TPS  │  │ Err Rate │ │
│  │ 1,234    │  │ 320 ms   │  │ 58.2     │  │ 0.8%     │ │
│  │ ↑ 12%    │  │ ↓ 8%     │  │ ↑ 4%     │  │ → flat   │ │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘ │
├─────────────────────────────────────────────────────────┤
│  Chart Row 1 (full width)                               │
│  ┌──────────────────────────────────────────────────┐   │
│  │ TTFT P50 / P95 line chart (per model)            │   │
│  │ x = time (range)  y = ms                          │   │
│  │ legend: click toggle, hover tooltip              │   │
│  └──────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────┤
│  Chart Row 2 (50/50 split)                              │
│  ┌──────────────────────┐  ┌─────────────────────────┐  │
│  │ Throughput (TPS)     │  │ Error Rate              │  │
│  │ area chart per model │  │ line chart per model    │  │
│  └──────────────────────┘  └─────────────────────────┘  │
├─────────────────────────────────────────────────────────┤
│  Table (insights)                                        │
│  ┌──────────────────────────────────────────────────┐   │
│  │ Model | Auths | P50 | P95 | TPS | Err% | Last    │   │
│  │ ... sortable, clickable to drill                  │   │
│  └──────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────┤
│  Drill Panel (slide-in from right when row clicked)    │
│  ┌──────────────────────────────────────────────────┐   │
│  │ Selected: gpt-5.5 → per-auth breakdown           │   │
│  │ - Auth A: line + sparkline                        │   │
│  │ - Auth B: line + sparkline                        │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

### 4.2 移动端（< 768px）

- 单列堆叠；KPI 卡片 2x2 → 1 列；charts 全宽
- 时间范围切换器移到顶部 toolbar（segmented control）
- drill panel 改为底部 sheet（占屏 60%）
- 不做 hamburger menu——dashboard 是 dense 信息屏，不是导航站

### 4.3 状态

| 状态 | 触发 | 表现 |
|---|---|---|
| **Loading 首次** | 页面 mount | 骨架屏（每块高度固定，淡灰） |
| **Loading 后续（refresh）** | refresh interval | 顶部进度条（不阻塞渲染） |
| **Empty** | `total=0` 且无历史 | "No data yet" + 解释说明，附「等待实时数据」 |
| **Error** | fetch 失败 | 卡片红色边框 + retry 按钮；保留 stale 数据 |
| **Auth 失败** | 401/403 | 顶部 sticky banner 提示「Management key 缺失/失效」并附 docs 链接 |

### 4.4 关键交互

- **时间窗口切换**：15m / 1h / 6h / 24h / 7d（注意 7d 需要 ≥ 7d retention）
- **自动刷新**：off / 30s / 1m / 5m（off = 手动 refresh 按钮）
- **多选对比**：在 insights table 行前加 checkbox，最多选 8 个 model / auth 同时画图
- **Chart tooltip**：hover 显示精确值；click 钉住 cursor（参考 uPlot `cursor.lock`）
- **Drill down**：点 table 行 → 右侧 panel 显示该行的细分

---

## 5. 数据源（不新增采集，复用已有 endpoint）

| 前端需要 | 来源 endpoint | 处理 |
|---|---|---|
| KPI 总数 / 平均 | `GET /stats/overview?window=...` | 原样使用 |
| TTFT P50/P95 时序 | `GET /stats/series?window=...&model=...` | 按 model 拆 series |
| TPS / Error rate 时序 | 同上 | 客户端计算（每 bucket） |
| Model 排行表 | `GET /stats/by-model?window=...` | 原样使用，加 sort |
| Drill 到 auth | `GET /stats/by-auth?window=...&model=...` | 客户端拼 `${provider}\|${model}` 前缀过滤 |
| 健康度（最近异常时段） | **新**：新增 `/stats/insights`？ | 见 §6 |

### 5.1 现有 endpoint 是否够用

- `/stats/series` 返回每个 `(provider, model, alias, auth_id) × bucket` 的一行 —— 这是 series 而非 aggregated timeline。需要**前端聚合**到 per-model
- `/stats/by-model` 已聚合，但缺分位数（S-3 review issue 已指出）
- `/stats/by-auth` 同上

### 5.2 前端聚合策略

每个 chart：
1. fetch `series?window=1m&from=<range start>`（limit 设大）
2. 客户端按 `splitKey(k)[1]`（model）分组
3. 对每个 model：把多个 auth 行的 P50/P95 加权平均（按 `count` 加权），或者**只展示分位数 = 加权 + 显示单一代表性 series**
4. uPlot x-axis = `window_start`，y-axis = ms/tps/err%

**数据量**：
- 1m window × 24h = 1440 buckets；5m window × 7d = 2016 buckets；1h window × 7d = 168 buckets
- 假设有 20 个活跃 series，每个 200 buckets → 4000 数据点
- uPlot 处理 4000 点 < 50ms —— 完全 OK

### 5.3 是否新增 endpoint？

**评估**：
- 现有 endpoints 够用，但**客户端聚合逻辑稍复杂**
- 新增 `/stats/insights`（server 端预计算 4 个 KPI：当前最快模型、最慢模型、最稳定、最近异常）能减少前端复杂度

**决定**：**新增一个轻量 endpoint `/stats/insights`**（server 端 O(N) 扫描 ~50K series 不昂贵），专门供 dashboard 的 KPI 卡片用。其他 chart 仍客户端聚合。理由：insights 卡片是 dashboard 第一屏就要显示的，省一个 round-trip 提升首屏感。

不做的：不要给 `/stats/series` 增加 server 端聚合参数（保持现有 MVP；客户端聚合灵活性更高）。

---

## 6. 配置项 schema

`config/config.go` 增量：

```go
type Config struct {
    // ... 既有字段
    DashboardEnabled   bool   `yaml:"dashboard_enabled"`    // 默认 true
    DashboardPath      string `yaml:"dashboard_path"`       // 默认 ""（用 host 默认 /v0/resource/plugins/my-cpa-stats-plugin/）
    DashboardReadonlyToken string `yaml:"dashboard_readonly_token"` // 默认 ""
}
```

| Key | 类型 | 默认 | 含义 |
|---|---|---|---|
| `dashboard_enabled` | bool | true | 整体开关；关闭后不注册 resource route |
| `dashboard_path` | string | "" | 暂不实现自定义路径（host 强制 `/v0/resource/plugins/<id>/`）；保留字段 |
| `dashboard_readonly_token` | string | "" | 可选 token；非空时 dashboard 需 `?token=...` 或 `Authorization: Bearer ...` 才返回；空则沿用 host 方式（仅 same-origin 用户能访问） |

### 6.1 鉴权方案

host 的 resource path 本身**不**走 management middleware。但同源部署下，dashboard 页面 JS 可读 localStorage 里的 management key（v7.x 提供 `localStorage` 中存 key 的能力——这是 CLIProxyAPI Management Center 的做法）。

**鉴权策略**（与 host 行为对齐）：
- 默认依赖 host 的同源策略 + 用户须登录 Management Center 后才访问
- 若用户开启 `dashboard_readonly_token`，则 dashboard JS 需先 POST token 到 `/stats/auth-check`，server 校验后下发临时 access cookie / header
- 这是**可选增强**，MVP 不实现。MVP 仅做"dashboard 路径依赖 host 同源保护"的文档说明

**简化决策（重要）**：**MVP 不实现自定义 token**。dashboard 直接挂在 `/v0/resource/plugins/my-cpa-stats-plugin/`，由 host 同源保护（resource path 必须 host 同源才能跑）。原因：

- 引入自定义 token 等于把 dashboard 单独暴露，与"不脱离 stats 插件职责"的原则冲突
- 若需要严格 token：用户在 host 端用 `remote-management` + `disable-control-panel` + reverse proxy auth 已经能保护
- 文档里说明风险并给出反向代理配置示例即可

---

## 7. 与现有代码的整合点

### 7.1 必须改的（stats 插件）

| 位置 | 修改 |
|---|---|
| `plugin/management_handler.go` `handleManagementRegister` | 给所有 `/stats/...` 移除 `Menu` 字段（避免被 host 转 resource route）；补充 `/stats/insights` 路由；新增 `Resources` 注册 dashboard |
| `plugin/management_handler.go` | 新增 `statsInsights()` 处理 insights endpoint |
| `plugin/aggregator/` | **不改**：现有 series/by-model/by-auth 数据已够前端 |
| `plugin/capabilities.go` `ConfigFields` | 增加 `dashboard_enabled` 字段 |
| `plugin/config/config.go` | 增加 `DashboardEnabled` 字段 |

### 7.2 不动的（保持隔离）

- `plugin/aggregator/` —— 现有数据完全够用
- `plugin/persist/` —— 不动
- `plugin/usage_handler.go` —— 不动
- `plugin/lifecycle.go` —— 不动

### 7.3 新增的（dashboard 专用子包）

```
plugin/dashboard/
├── embed.go             ← //go:embed web/dist/*
├── handler.go           ← ResourceRoute Handler impl：分发 /、/assets/*
├── static.go            ← asset lookup + MIME + cache headers
└── insights.go          ← 服务端 /stats/insights 计算
```

前端资产：

```
plugin/web/dist/
├── index.html
├── uPlot.iife.min.js
├── uPlot.min.css
├── app.js               ← dashboard 逻辑（~500-800 行 vanilla JS）
└── app.css              ← 样式（~200 行）
```

---

## 8. 任务拆分（适合 iterator 一次完成）

| # | 任务 | 估时 |
|---|---|---|
| 1 | 调整 stats 插件：移除 `/stats/*` 的 `Menu`，新增 `Resources` 注册 + `/stats/insights` endpoint | S |
| 2 | 实现 `plugin/dashboard/embed.go` + `handler.go` + `static.go` | M |
| 3 | 实现 `plugin/dashboard/insights.go` 服务端计算 | S |
| 4 | 把 `uPlot.iife.min.js` + `uPlot.min.css` vendored 到 `plugin/web/dist/` | S |
| 5 | 写 `plugin/web/dist/index.html` 骨架 + toolbar + KPI cards | M |
| 6 | 写 `plugin/web/dist/app.css`：design tokens、layout、dark mode | M |
| 7 | 写 `plugin/web/dist/app.js` 的 fetch + chart 渲染 + tooltip 插件 + resize observer | L |
| 8 | 实现 insights table + drill panel + 多选对比 | M |
| 9 | 移动端断点 + touch 缩放插件 | S |
| 10 | 端到端验证（CLIProxyAPI dev 实例 + 真实流量） | M |

iterator 一次完成：任务 1-9 由一个 iterator agent 跑完；任务 10 由用户实机验证。

---

## 9. 可访问性 / 移动端要点

- **颜色对比度**：默认亮色主题文字 #1a1a1a / 背景 #fff，对比度 ≥ 7:1（WCAG AAA）；暗色 #e8e8e8 / #0d1117
- **键盘导航**：时间范围切换 / chart legend 用 `<button>`；Enter 触发；focus 环可见
- **屏幕阅读器**：KPI 卡片 `role="group" aria-label="Total requests, 1234, +12%"`；charts 上覆盖 `aria-label="Line chart of TTFT p50..."`
- **响应式断点**：768px / 1024px；手机单列，平板 2 列，桌面 4 列 KPI
- **触摸**：uPlot touch zoom plugin 启用；避免依赖 hover（所有 hover 信息在 click 时也能用键盘重现）
- **prefers-reduced-motion**：关闭 chart 过渡动画

---

## 10. 安全要点

| 风险 | 缓解 |
|---|---|
| XSS via model/auth 名字（带 `<script>`） | host **resource 响应不**经 `htmlsanitize`；前端 JS **必须**用 `textContent`（非 `innerHTML`）插入所有动态字符串。Dashboard JS 用 vanilla DOM API，避免框架自动 escape 的不确定性。 |
| CSP | `Content-Security-Policy: default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:`。`script-src 'self'` 阻断任何 inline script。 |
| Clickjacking | `X-Frame-Options: DENY` |
| MIME sniff | `X-Content-Type-Options: nosniff` |
| Auth key 泄露 | dashboard **不**持久化 key 到 localStorage；每次 fetch 由 host 同源策略保护 |
| 任意 path 访问 `assets/` | `handler.static.go` 维护白名单：仅放行 `index.html / app.js / app.css / uPlot.iife.min.js / uPlot.min.css` |

### 10.1 token 传递（若启用 `dashboard_readonly_token`）

> MVP 不实现。文档说明：推荐通过 reverse proxy（如 nginx `auth_request` 或 oauth2-proxy）保护 dashboard URL。

---

## 11. 风险与备选

| 风险 | 备选 |
|---|---|
| uPlot 学习曲线略陡（hooks 系统） | 退回 Chart.js（多 60KB + streaming 衰减）；或 ECharts（+300KB 但开箱即用） |
| embed.FS 引入 ~150 KB 二进制膨胀 | 可接受（c-shared .so 已 ~5MB+）；不引入运行时下载 |
| `series` 端点返回数据量大（7d × 1m × 20 series ≈ 200K 点） | 客户端聚合在 worker 内（不阻塞 UI）；或后端加 series 聚合 endpoint（follow-up） |
| `/stats/insights` 在 series 数大时（50K）扫描慢 | 增量计算：每个 insight 字段都有 lastComputedAt，60s cache |
| 移动端 uPlot 触摸缩放略糙 | 接受；提供 zoom-out reset 按钮 |
| 同源限制导致 cross-origin 部署打不开 dashboard | 文档说明：dashboard 需 CLIProxyAPI 与浏览器同源；cross-origin 部署应使用 reverse proxy auth |

---

## 12. 设计参考（克制、信息密度高）

- **GitHub Pulse**：贡献热力图风格（暗主题 + 1px 网格 + 单一强调色）
- **Vercel Analytics**：bento 4 列 + delta 徽章 + 时间范围 + 时间序列图
- **Cal.com Insights**：折线图 + 多 series 图例 toggle + drill 到具体 booking
- **Supabase Studio**：顶栏控制 + 表格 + 详情侧栏；移动端折叠
- **Grafana 内置 dashboards**：多 panel / 时间范围共享；变量下拉

我们 dashboard 的视觉风格取 Vercel Analytics + Supabase Studio 的混搭：

- 顶栏：左侧 plugin 标题，右侧时间范围 + refresh
- KPI 卡片：4 列 grid，无图标（小密度屏），数字 + delta 徽章
- Chart：占满列宽，单色主调 + 图例多色
- Table：斑马纹、最右列 last seen；行 hover 高亮
- 颜色调色板：主色 `#3b82f6`（Tailwind blue-500），成功 `#10b981`，警告 `#f59e0b`，错误 `#ef4444`，中性灰 7 阶

---

## 13. 结论（plan 输入）

1. **图表库**：uPlot（IIFE min 35 KB）—— 单文件 + TimeSeries 性能 + canvas 内存稳
2. **资源挂载**：`Resources` 注册 `GET /v0/resource/plugins/my-cpa-stats-plugin/{,assets/*}`；前端资产 vendored 到 `plugin/web/dist/`
3. **后端增量**：仅新增 `/stats/insights`（KPI 卡片 server-side 聚合）；其他复用
4. **数据流**：页面 fetch → management endpoint → client 聚合 → uPlot render；auto-refresh 用 `setInterval` + abort controller
5. **配置**：`dashboard_enabled: true|false`；其他字段（path / token）保留位但 MVP 不实现
6. **架构**：`plugin/dashboard/` 子包；前端 vanilla JS（无框架）；CSP/XSS 加固
7. **测试**：dashboard 自身集成测试不写（前端 E2E 投入过大）；改用：JS 单元测试（assertion on DOM）+ 手动浏览器验证
8. **非目标**：不做配置中心、用户管理、跨主机聚合、Prometheus 导出