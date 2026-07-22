# 260722-dashboard · Plan

> 为 stats 插件构建一个真正好用、可视化趋势分析的内嵌 dashboard。
> 关联调研：`260722-dashboard.research.md` · 前置：`docs/features/260722-stats-plugin/`

---

## 1. 目标与范围

### 1.1 用户故事

> 作为 CLIProxyAPI 运维，我打开 `http://host/v0/resource/plugins/my-cpa-stats-plugin/`（即 Management Center 旁的 "Stats Dashboard" 入口），看到一个内嵌 dashboard：
>
> - **趋势图**：TTFT P50/P95、TPS、Error Rate 时间序列（15m / 1h / 6h / 24h / 7d 可切）
> - **多 model 对比**：表 + 图同时展示，点行 drill 到该 model 的 auth 列表
> - **关键洞察卡片**：当前最快模型、最慢模型、最稳定模型、最近异常时段
> - **图表交互顺滑**：缩放、悬停 tooltip、图例 toggle、键盘可达
> - **移动端可读**：单列堆叠 + 触摸缩放

### 1.2 范围内

- 新增 HTTP dashboard 页面（vanilla HTML/JS/CSS，单 vendored lib + 嵌入二进制）
- 通过 `ResourceRoute` 暴露在 `/v0/resource/plugins/my-cpa-stats-plugin/`
- 复用 stats 已有 `/stats/{overview,series,by-model,by-auth}` 端点
- 新增 1 个轻量后端 endpoint：`/stats/insights`（server 端聚合 KPI）
- 配置：`dashboard_enabled` 开关
- 与现有 stats 插件共用 `pluginState`（不重写 stats）

### 1.3 范围外（明确不做）

- 用户管理、配置中心、跨主机聚合、Prometheus 导出
- 自定义 token 鉴权（依赖 host 同源 + reverse proxy）
- npm/yarn 构建链（前端 vendored，无 bundle）
- 服务端 series 聚合（客户端聚合；服务端保留 MVP 不变）
- i18n（仅英文；后续可加）

### 1.4 与现有 stats 插件的耦合

| 耦合点 | 处理 |
|---|---|
| 路由注册 | 在 `handleManagementRegister` 同函数内追加 `Resources` 字段；不动其他 |
| Aggregator / Bucket | **零修改** —— dashboard 只读 snapshot |
| ConfigYAML | 增量加 `DashboardEnabled` 一个字段 |
| pluginState 共享 | 复用；dashboard 仅持有 `*pluginState` 引用读 `agg` / `cfg` |
| 编译产物 | 同一 DLL/SO；前端资产 embed 进二进制 |

---

## 2. 架构总览

```
┌────────────────────────────────────────────────────────────────┐
│ CLIProxyAPI Host                                               │
│                                                                │
│  /v0/management/stats/*      ──auth──▶  management.handle      │
│  /v0/resource/plugins/...    ──public─▶  resource.handle        │
│                                       │                        │
│                                       ▼                        │
│                              ┌────────────────┐                │
│                              │ stats-plugin   │                │
│                              │  .dll/.so/.dylib│                │
│                              │                │                │
│                              │  aggregator    │                │
│                              │       ▲        │                │
│                              │       │        │                │
│                              │  dashboard ────┘                │
│                              │  handler       │                │
│                              │  ├─ /stats/insights (compute)   │
│                              │  └─ /resource/... (serve FS)    │
│                              │       ▲                        │
│                              │       │                        │
│                              │  embed.FS (web/dist/*)         │
│                              └────────────────┘                │
└────────────────────────────────────────────────────────────────┘

Browser → /v0/resource/plugins/my-cpa-stats-plugin/
        → 同一 DLL .so 内 serve index.html
        → <script src="assets/uPlot.iife.min.js"></script>
        → fetch /v0/management/stats/series?window=1m&...
        → Authorization: Bearer <key from host localStorage>
        → render uPlot charts
```

### 2.1 数据流（dashboard JS）

```
mount()
  ├─ fetch GET /v0/management/stats/overview   → KPI total/success/failed
  ├─ fetch GET /v0/management/stats/insights   → 4 KPI cards
  ├─ fetch GET /v0/management/stats/series     → TTFT/TPS/Err 时间序列
  ├─ fetch GET /v0/management/stats/by-model   → 排行表
  └─ fetch GET /v0/management/stats/by-auth    → drill panel

window range change → refetch all (abort previous)
auto-refresh tick   → refetch series + overview + insights only (cheap)
table row click     → refetch by-auth?model=X (drill)
```

### 2.2 关键不变量

- dashboard JS **不**依赖内联 `<script>` —— 全部 `src=` 引用，匹配 CSP `script-src 'self'`
- 所有动态字符串插入 DOM 走 `textContent`（无 `innerHTML`）
- `/stats/insights` 计算开销 O(N) 单次扫描；默认 N < 50K
- embed.FS 在编译期固化，运行时无法修改；reload 二进制才生效
- Resource handler 是无状态的纯 IO；dashboard 子包不引入新的 goroutine

---

## 3. 模块划分

### 3.1 后端（Go）—— 增量

```
plugin/
├── dashboard/                       ← 新增子包（与 stats 共用 pluginState）
│   ├── embed.go                     ← //go:embed web/dist/*
│   ├── handler.go                   ← ResourceRoute.Handler 实现（dispatch /、/assets/*）
│   ├── static.go                    ← asset lookup + MIME + cache headers
│   ├── insights.go                  ← /stats/insights 计算
│   └── web/dist/                    ← vendored frontend
│       ├── index.html
│       ├── uPlot.iife.min.js
│       ├── uPlot.min.css
│       ├── app.js
│       └── app.css
├── management_handler.go            ← 修改：移除 Menu / 追加 Resources / 追加 insights
├── capabilities.go                  ← 修改：ConfigFields 加 dashboard_enabled
└── config/config.go                 ← 修改：Config 加 DashboardEnabled
```

### 3.2 前端（vanilla JS）—— 全量新增

```
plugin/dashboard/web/dist/
├── index.html            ← <head>、CSP-friendly、无内联
├── app.js                ← 模块化 vanilla（无 IIFE 框架；IIFE 包一层供 script 引用）
│   ├── fetch.js          ← auth header + abort controller + retry
│   ├── state.js          ← 时间范围、选中 model、auto-refresh
│   ├── charts.js         ← uPlot 封装：series → data 转换、tooltip plugin、resize obs
│   ├── touch.js          ← pinch zoom plugin（抄 uPlot zoom-touch.html）
│   ├── kpi.js            ← KPI cards 渲染
│   ├── insights_table.js ← 排行表 + sort + 行 click
│   ├── drill.js          ← 右侧 panel 渲染
│   ├── empty.js          ← 空/错误/加载状态
│   └── app.js            ← 顶层 wiring
└── app.css               ← design tokens + grid + dark mode + 移动端
```

### 3.3 包结构注意

- dashboard 子包与 stats 共用 `pluginState`（用 package-internal 引用；不导出）
- `plugin/dashboard/web/` 与 `plugin/dashboard/` 同包——web 在 Go 里是 `//go:embed` 源，运行时经 `fs.Sub()` 暴露
- 不拆 `internal/dashboard` —— 单包最简；c-shared 不在意目录深度

---

## 4. 关键接口契约

### 4.1 `plugin.dashboard.handler` 实现 `pluginapi.ManagementHandler`

```go
type resourceHandler struct{ fs fs.FS }

func (h *resourceHandler) HandleManagement(ctx context.Context, req pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
    // dispatch by req.Path
    // /v0/resource/plugins/<id>/              → web/dist/index.html
    // /v0/resource/plugins/<id>/assets/...    → web/dist/assets/...
    // other                                  → 404
    return h.serve(req.Path)
}
```

> ResourceRoute.Handler 字段在 host 端会被 `rpc_client.go` 覆盖；我们不直接持有 Handler 字段，但**类型签名必须实现** `ManagementHandler` 接口（编译期保险）。

实际注册写法（与现有 stats 一致）：

```go
resources := []pluginapi.ResourceRoute{
    {Path: "/", Menu: "Stats Dashboard", Description: "Trends and insights."},
    {Path: "/assets/*", Description: "Static assets."},
}
```

### 4.2 静态资源服务（`static.go`）

```go
var allowList = map[string]bool{
    "index.html":           true,
    "app.js":               true,
    "app.css":              true,
    "uPlot.iife.min.js":    true,
    "uPlot.min.css":        true,
}

func (h *resourceHandler) serve(reqPath string) (pluginapi.ManagementResponse, error) {
    sub, _ := strings.CutPrefix(reqPath, resourceBasePath)  // "/v0/resource/plugins/<id>"
    sub = strings.TrimPrefix(sub, "/")
    if sub == "" {
        return serveFile(h.fs, "index.html", "no-cache")
    }
    if !strings.HasPrefix(sub, "assets/") {
        return notFound()
    }
    name := strings.TrimPrefix(sub, "assets/")
    if !allowList[name] {
        return notFound()
    }
    return serveFile(h.fs, "assets/"+name, "public, max-age=3600")
}

func serveFile(fsys fs.FS, name, cache string) (pluginapi.ManagementResponse, error) {
    data, err := fs.ReadFile(fsys, name)
    if err != nil { return notFound() }
    h := http.Header{}
    h.Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
    h.Set("Cache-Control", cache)
    h.Set("X-Content-Type-Options", "nosniff")
    h.Set("X-Frame-Options", "DENY")
    if strings.HasSuffix(name, ".html") {
        h.Set("Content-Security-Policy",
            "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
    }
    return pluginapi.ManagementResponse{StatusCode: 200, Headers: h, Body: data}, nil
}
```

### 4.3 `/stats/insights`（`insights.go`）

```go
type insightsResponse struct {
    FastestModel    insightRow `json:"fastest_model"`     // by P50
    SlowestModel    insightRow `json:"slowest_model"`
    MostStableModel insightRow `json:"most_stable_model"` // by P95/P50 ratio
    LastAnomaly     insightRow `json:"last_anomaly"`      // latest high-fail window
    GeneratedAt     string     `json:"generated_at"`
}

type insightRow struct {
    Model      string  `json:"model"`
    Window     string  `json:"window"`
    Value      float64 `json:"value"`
    Unit       string  `json:"unit"`       // "ms" / "%" / "tps"
    Detail     string  `json:"detail"`     // e.g. "12 auths, last seen 2m ago"
}
```

算法：

```go
func (p *pluginState) statsInsights() pluginapi.ManagementResponse {
    snap := p.agg.Snapshot()
    m1 := snap[time.Minute]
    var ins insightsResponse
    // scan m1 once → collect per-model stats
    type acc struct { sumLat float64; sumP95 float64; n int; lastT time.Time; auths map[string]struct{} }
    agg := map[string]*acc{}
    for k, b := range m1 {
        model := splitKey(k)[1]
        if model == "" { continue }
        a := agg[model]
        if a == nil { a = &acc{auths: map[string]struct{}{}}; agg[model] = a }
        a.sumLat += float64(b.AvgLatency().Milliseconds()) * float64(b.Count)
        a.sumP95 += float64(b.Percentile(0.95).Milliseconds()) * float64(b.Count)
        a.n += int(b.Count)
        a.lastT = maxTime(a.lastT, b.LastSampleAt)
        a.auths[splitKey(k)[3]] = struct{}{}
    }
    // pick fastest by sumLat/n, slowest = inverse, stable by p95/sumLat, anomaly = highest fail rate
    // ...
    ins.GeneratedAt = time.Now().Format(time.RFC3339)
    return jsonResponse(http.StatusOK, ins)
}
```

实现细节：

- 缓存：60s TTL（`p.insightsCache` map）；cache 命中直接返回
- O(N) 扫描 N = 当前 series 数；50K 时 < 5ms
- 复用 `aggregator.SplitSeriesKey`

### 4.4 修改现有 stats routes —— 移除 `Menu`

```go
// 之前（plugin/management_handler.go:30-37）
{Method: "GET", Path: "/stats/overview", Menu: "Stats", ...}  // ← Menu 字段

// 之后
{Method: "GET", Path: "/stats/overview", ...}                  // ← Menu 留空
```

原因：host 对 `Menu != ""` 的 GET 自动转 resource route（`routeDeclaresLegacyMenuResource`），导致 JSON API 同时暴露在 `/v0/management/stats/overview` 和 `/v0/resource/plugins/.../stats/overview`，前者才是 auth'd 路径。移除 `Menu` 后强制走 management API。

### 4.5 注册 Resources

```go
return okEnvelope(pluginapi.ManagementRegistrationResponse{
    Routes: routes,        // 不含 Menu
    Resources: []pluginapi.ResourceRoute{
        {Path: "/", Menu: "Stats Dashboard", Description: "Trends and insights for CLIProxyAPI usage."},
        {Path: "/assets", Description: "Static assets (JS/CSS)."},  // prefix-style
    },
})
```

注：ResourceRoute.Path 不支持通配符；`/assets` 会成为 `assets` 子前缀的入口，由 handler 内部按 `req.Path` 子串匹配。

### 4.6 ConfigYAML 示例

```yaml
plugins:
  configs:
    my-cpa-stats-plugin:
      enabled: true
      retention_minutes: 1440
      persist_path: "stats.json"
      persist_interval_sec: 30
      cardinality_limit: 50000
      dashboard_enabled: true           # 新增：默认 true；false 则不注册 Resources
```

---

## 5. 数据模型（前端）

### 5.1 Series aggregation（前端）

```js
// 输入：/stats/series 的多个 bucket
// 输出：uPlot data = [timestamps[], p50_series[], p95_series[]]
function aggregateSeries(rows, windowSize) {
  // 1. 按 model 分组
  const byModel = {};
  for (const r of rows) {
    const [, model] = splitKey(r.key);
    byModel[model] ??= [];
    byModel[model].push(r);
  }
  // 2. 按 bucket_start 对齐；缺失填 null（spanGaps: false）
  // 3. P50/P95 加权平均（权重 = count）
  // 4. TPS = sum_output_tokens / (sum_latency - sum_ttft) per bucket
  // 5. Error Rate = failed / count per bucket
}
```

### 5.2 KPI cards（来自 `/stats/insights`）

```js
// input: insightsResponse
insights.fastest_model  → "gpt-5.5 — 320 ms P50 across 3 auths"
insights.slowest_model  → "claude-opus — 1.2s P50 across 2 auths"
insights.most_stable    → "gpt-5-mini — P95/P50 = 1.3× across 8 auths"
insights.last_anomaly   → "12:30 — gemini-pro, 18% error rate over 5min"
```

### 5.3 Insights table

```js
// input: /stats/by-model?window=1m
const cols = [
  { key: "model", label: "Model" },
  { key: "authCount", label: "Auths", compute: (row, allRows) => count auths in same model },
  { key: "p50", label: "P50", format: ms },
  { key: "p95", label: "P95", format: ms },
  { key: "tps", label: "TPS", format: tps },
  { key: "errorRate", label: "Err%", format: pct },
  { key: "lastSeen", label: "Last", format: relTime },
];
```

行点击 → 触发 drill fetch (`by-auth?model=X`)。

---

## 6. 配置项 & 默认值

| Key | 类型 | 默认 | 含义 |
|---|---|---|---|
| `dashboard_enabled` | bool | true | 整体开关；关闭后 `Resources` 不注册 |
| `dashboard_path` | string | "" | 预留字段（host 强制 resource 路径；MVP 不实现自定义） |
| `dashboard_readonly_token` | string | "" | 预留字段（依赖 host 同源；MVP 不实现） |

### 6.1 关闭行为

`dashboard_enabled: false` 时：

- `handleManagementRegister` 返回的 `Resources` 为空（无 dashboard 入口）
- `/stats/insights` 仍返回 404（与其他 stats 路径保持一致）

---

## 7. 与现有代码的整合点（重要）

### 7.1 必须改的（stats 插件）

| 位置 | 修改 | 原因 |
|---|---|---|
| `plugin/management_handler.go` `handleManagementRegister` | 给所有 `/stats/...` GET 路由**移除 `Menu` 字段**（`overview/series/by-model/by-auth/keys/config`）；补充 `/stats/insights` 路由；新增 `Resources` 注册 dashboard | **修复潜在 bug**：现有 `Menu: "Stats"` 被 host 转为 resource 路径，绕过鉴权；详见 research §1.3 |
| `plugin/management_handler.go` | 新增 `statsInsights()` 处理 insights endpoint | dashboard KPI 卡片用 |
| `plugin/aggregator/` | **不改**：现有 series/by-model/by-auth 数据已够前端 | 最小改动 |
| `plugin/capabilities.go` `ConfigFields` | 增加 `dashboard_enabled` 字段 | 暴露给 host UI 表单 |
| `plugin/config/config.go` | 增加 `DashboardEnabled` 字段 | 开关 |
| `README.md` | **更新**：endpoint 路径描述从 `/v0/resource/plugins/my-cpa-stats-plugin/stats/...` 改回 `/v0/management/stats/...` | 与本次修复一致 |

### 7.2 不动的（保持隔离）

- `plugin/aggregator/` —— 现有数据完全够用
- `plugin/persist/` —— 不动
- `plugin/usage_handler.go` —— 不动
- `plugin/lifecycle.go` —— 不动

### 7.3 新增的（dashboard 专用子包）

```
plugin/dashboard/
├── embed.go             ← //go:embed web/dist/*
├── handler.go           ← resource handler：dispatch /、/assets/*
├── static.go            ← asset lookup + MIME + cache headers
├── insights.go          ← 服务端 /stats/insights 计算
└── web/dist/            ← 前端资产（vendored）
    ├── index.html
    ├── uPlot.iife.min.js
    ├── uPlot.min.css
    ├── app.js
    └── app.css
```

### 7.4 并发模型

无新增 goroutine：

- Resource handler 由 host 同步调用（`internal/pluginhost/management.go:286`）；无状态
- `/stats/insights` 一次性读 `agg.Snapshot()` 后立即返回；60s TTL cache 由 `sync.RWMutex` 保护
- 前端 `setInterval` 在主线程；不影响后端

---

## 8. 前端设计要点

### 8.1 视觉风格

```
颜色（亮）：
  --bg:        #ffffff
  --bg-alt:    #f8fafc
  --fg:        #0f172a
  --fg-mute:   #64748b
  --border:    #e2e8f0
  --accent:    #3b82f6  (主色 / 链接 / 选中)
  --success:   #10b981
  --warn:      #f59e0b
  --error:     #ef4444
  --series-1:  #3b82f6
  --series-2:  #8b5cf6
  --series-3:  #ec4899
  --series-4:  #10b981
  --series-5:  #f59e0b

颜色（暗）：
  --bg:        #0d1117
  --bg-alt:    #161b22
  --fg:        #e6edf3
  --fg-mute:   #8b949e
  --border:    #30363d
  --accent:    #58a6ff
  其他 series 同亮色
  切换：`prefers-color-scheme: dark` + 手动 toggle
```

### 8.2 布局断点

| 断点 | 宽度 | 行为 |
|---|---|---|
| mobile | < 768px | 1 列；KPI 1×4 纵向堆；charts 全宽；drill panel 底部 sheet |
| tablet | 768-1023px | 2 列 KPI；charts 50/50；drill right |
| desktop | ≥ 1024px | 4 列 KPI；charts 2+1 布局；drill right panel 360px |

### 8.3 关键交互细节

| 交互 | 行为 |
|---|---|
| 时间范围切换 | segmented control：15m / 1h / 6h / 24h / 7d；点击立即 refetch |
| 自动刷新 | segmented：off / 30s / 1m / 5m；off 时显手动 refresh 按钮 |
| Chart legend 点击 | 切换 series 可见性（uPlot `series[i].show = !show`） |
| Chart hover | tooltip 显示精确值（来自 uPlot cursor） |
| Chart 双击 | 重置缩放（uPlot `setScale('x', null)`） |
| Chart 触摸 pinch | zoom-touch plugin 启用 |
| Table 列头点击 | sort asc/desc/none |
| Table 行点击 | 选中高亮 + drill panel 打开（若已开则替换） |
| Drill panel 关闭 | × 按钮 / Esc 键 |
| 键盘 | Tab 顺序：toolbar → KPI cards → charts → table；Enter 触发点击 |

### 8.4 状态处理

| 状态 | UI |
|---|---|
| 首次加载 | 骨架屏（每块高度固定 80px / 280px，淡灰 `#f1f5f9` 闪烁） |
| Refresh 中 | 顶部 2px 进度条（不阻塞渲染） |
| Empty（无数据） | "No data yet. Waiting for first request to be aggregated." + 倒计时显示 |
| Error | KPI 卡片红边 + retry 按钮；保留 stale 数据；底部 toast 显示错误 |
| Auth 失败 | 顶部 sticky 红 banner：「Management key 缺失或失效。请确认 Management Center 已登录。」 |

### 8.5 XSS 加固

```js
// 严禁 innerHTML 插入动态字符串
// 允许：
el.textContent = modelName;
el.setAttribute('aria-label', `${model}: ${p50}ms`);

// 允许 innerHTML 仅用于纯静态模板字符串（无用户数据）
panel.innerHTML = `<button class="close">×</button>`;
```

---

## 9. 安全要点

| 风险 | 缓解 |
|---|---|
| XSS（model/auth 名带 `<script>`） | 全部 `textContent`；CSP `script-src 'self'`（禁 inline）；CSP `default-src 'self'`（禁 inline event handler） |
| Clickjacking | `X-Frame-Options: DENY` |
| MIME sniff | `X-Content-Type-Options: nosniff` |
| 任意 asset path 探测 | 白名单 `index.html` / `app.js` / `app.css` / `uPlot.iife.min.js` / `uPlot.min.css` |
| 路径穿越 | `strings.HasPrefix(sub, "assets/")` + 白名单；不拼接用户输入 |
| 慢请求 | 前端 `AbortController` + 8s timeout；超时显示 partial state + retry |
| 持久 token 泄露 | dashboard **不**写 localStorage；每次 fetch 取 host 提供的 sessionStorage（若 host 有） |
| 跨 origin 部署 | dashboard 无法在跨 origin 浏览器中运行；文档说明 |

### 9.1 CSP 实际生成

```go
h.Set("Content-Security-Policy",
    "default-src 'self'; "+
    "script-src 'self'; "+
    "style-src 'self' 'unsafe-inline'; "+   // uPlot CSS 用 inline <style> 注入
    "img-src 'self' data:; "+
    "connect-src 'self'; "+
    "frame-ancestors 'none'; "+
    "base-uri 'self'; "+
    "form-action 'none'")
```

> `'unsafe-inline'` 仅用于 style 是为了让 `uPlot` 自身注入的 SVG-as-CSS `style` 合法；script 端严格禁 inline。

---

## 10. 构建产物

### 10.1 embed.FS 大小

| 文件 | 大小（gzip 前） |
|---|---|
| uPlot.iife.min.js | 35 KB |
| uPlot.min.css | 1.8 KB |
| index.html | ~6 KB |
| app.js | ~30-40 KB |
| app.css | ~5-8 KB |
| **总计** | **~80-95 KB** |

嵌入二进制：原 DLL ~5 MB → +0.1 MB。**完全可接受**。

### 10.2 构建命令（与现有 stats 一致）

`cmd/build/main.go` 加一行把 `web/dist/` 也嵌入；不需要改。

```bash
go build -buildmode=c-shared -o bin/windows/amd64/my-cpa-stats-plugin.dll ./plugin
```

### 10.3 vendored lib 更新

```bash
# 一次性；从 uPlot release 下载
curl -L https://github.com/leeoniya/uPlot/releases/latest/download/uPlot-iife.min.js -o plugin/dashboard/web/dist/uPlot.iife.min.js
curl -L https://github.com/leeoniya/uPlot/releases/latest/download/uPlot.min.css       -o plugin/dashboard/web/dist/uPlot.min.css
```

> 不在 `cmd/build/main.go` 自动化这一步；vendored 文件提交到 git；发版时手动更新。

---

## 11. 测试要点

### 11.1 后端（Go）

- `plugin/dashboard/handler_test.go`：mock `ManagementRequest.Path`，断言返回文件 / MIME / cache headers
- `plugin/dashboard/insights_test.go`：注入固定 sample，断言 4 个 insight 字段正确选出
- `plugin/dashboard/static_test.go`：白名单边界（已知 5 文件 + 越界 3 case）
- 现有 stats 测试不变

### 11.2 前端（手动 + smoke）

不引入 jsdom + 框架测试栈。验证清单：

- [ ] `index.html` 在 `/v0/resource/plugins/my-cpa-stats-plugin/` 返回 200 + `text/html`
- [ ] 4 个 JS/CSS asset 返回 200 + 正确 MIME
- [ ] 任何不在白名单的 path 返回 404
- [ ] 路径含 `..` 返回 404（host 端 `normalizeResourceRoute` 已拦，再加 server 端二次防御）
- [ ] 真实浏览器（Chrome/Firefox/Safari）：渲染、缩放、tooltip、touch、dark mode、空状态

### 11.3 端到端

- 启动 CLIProxyAPI dev 模式
- 加载本插件
- 浏览器访问 dashboard URL
- 触发真实模型请求若干次
- 观察 dashboard：KPI 数字更新、chart series 出现、tooltip 显示真实值
- 移动端 viewport（DevTools）确认响应式

---

## 12. 已知风险与备选

| 风险 | 备选 |
|---|---|
| uPlot 学习曲线 | 退回 Chart.js（+60KB + streaming 衰减） |
| 客户端聚合 series 多时 O(N) 卡顿 | worker 内聚合；或 server 端聚合 endpoint（follow-up） |
| `/stats/insights` 50K series 时扫描慢 | 60s cache 已加；可继续加增量更新 |
| 移动端触摸 zoom 精度 | 提供 reset 按钮（reset zoom） |
| 跨 origin 部署打不开 | 文档说明；reverse proxy auth 兜底 |
| embed.FS 二进制不可热更新 | reload 二进制即可；与现有 stats 插件一致 |

---

## 13. 实施任务拆分（实施时使用）

> 不是文档，是给后续 `/workflow-implement-review` 用的检查清单。

**iterator 一次完成：**

1. `plugin/dashboard/embed.go` — `//go:embed web/dist/*` + `embed.FS`
2. `plugin/dashboard/static.go` — `serveFile` + 白名单 + headers
3. `plugin/dashboard/handler.go` — `ResourceHandler` 实现 `ManagementHandler`
4. `plugin/dashboard/insights.go` — `/stats/insights` 计算 + 60s cache
5. 修改 `plugin/management_handler.go`：移除 Menu、追加 insights 路由、追加 Resources 注册
6. 修改 `plugin/config/config.go`：加 `DashboardEnabled`
7. 修改 `plugin/capabilities.go`：加 `dashboard_enabled` ConfigField
8. vendored `uPlot.iife.min.js` + `uPlot.min.css` 到 `plugin/dashboard/web/dist/`
9. `plugin/dashboard/web/dist/index.html` — 骨架 + CSP meta
10. `plugin/dashboard/web/dist/app.css` — design tokens + 网格 + dark mode + 移动断点
11. `plugin/dashboard/web/dist/app.js` — fetch + state + chart + KPI + table + drill + 空错状态
12. Go 单元测试（handler / static / insights）
13. 端到端验证（CLIProxyAPI dev 实例 + 真实流量）

**用户实机验证（validation.md）：**

14. dashboard 入口在 host Management Center 旁可见
15. KPI / 趋势 / 表格 / drill 在 15m / 1h / 6h / 24h / 7d 五档均工作
16. Chrome / Safari / Firefox 渲染一致；移动端 viewport 单列可读
17. 触摸 pinch 缩放、双击 reset、键盘 Tab/Enter 全部可达
18. 空数据 / 错误数据 / 鉴权失败三种状态正确切换

---

## 14. 对齐用户预期（待用户确认）

请用户核对以下决策：

1. **图表库 uPlot** —— 单文件 35KB，TimeSeries 专用 canvas，体积/性能/契合度最优
2. **前端 vanilla JS + 嵌入** —— 不引入 npm/React；用 `//go:embed` 把 `web/dist/*` 编进二进制
3. **挂载点 `/v0/resource/plugins/my-cpa-stats-plugin/`** —— host 浏览器资源路径；不走 management 鉴权
4. **复用 stats 已有 `/stats/{overview,series,by-model,by-auth}`** —— 不新增采集；客户端聚合
5. **新增 1 个轻量 endpoint `/stats/insights`** —— 服务端聚合 4 个 KPI 卡片
6. **移除现有 `/stats/*` 路由的 `Menu` 字段** —— 避免被 host 当 legacy resource route 转译
7. **配置仅 `dashboard_enabled`** —— 自定义 token / 路径 MVP 不实现
8. **依赖 host 同源 + reverse proxy auth** —— 不实现自定义 dashboard token
9. **不写前端 E2E 测试** —— Go 单测覆盖 handler/static/insights；前端靠浏览器手动 + smoke

如有任一不同意，请告知，我重新设计。