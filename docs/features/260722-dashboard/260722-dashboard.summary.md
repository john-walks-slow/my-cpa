# 260722-dashboard · Summary

## 背景

在 stats 插件基础上构建内嵌可视化 dashboard，提供趋势分析、KPI 卡片、模型对比和下钻功能。同时修复现有 `/stats/*` 路由因 `Menu` 字段被 host 转为 resource 路径而绕过 management 鉴权的 bug。

## 实施内容

### Bug 修复

- 移除所有 `/stats/*` GET 路由的 `Menu: "Stats"` 字段，使其正确注册为 management 路由（需鉴权）

### 后端（Go）

| 文件 | 变更 |
|------|------|
| `plugin/config/config.go` | 新增 `DashboardEnabled *bool` 字段 + `IsDashboardEnabled()` 方法（默认 true） |
| `plugin/capabilities.go` | 新增 `dashboard_enabled` ConfigField |
| `plugin/management_handler.go` | 移除 Menu；新增 `/stats/insights` 路由 + handler（60s cache，`dashboard_enabled=false` 时不注册）；新增 Resources 注册；新增 resource path dispatch；`by-auth` 支持 `model` 过滤 |
| `plugin/lifecycle.go` | pluginState 增加 insights cache 字段 |
| `plugin/dashboard/embed.go` | `//go:embed all:web/dist` |
| `plugin/dashboard/handler.go` | 静态资源服务：白名单 5 文件、MIME、Cache-Control、CSP、安全头 |
| `plugin/dashboard/insights.go` | `ComputeInsights()`：O(N) 扫描 1m 窗口，计算 fastest/slowest/most-stable/last-anomaly |

### 前端（vanilla JS，无构建链）

| 文件 | 说明 |
|------|------|
| `web/dist/index.html` | 页面骨架，CSP-friendly（无 inline script） |
| `web/dist/app.css` | Design tokens、响应式布局（mobile/tablet/desktop）、暗色模式、骨架屏 |
| `web/dist/app.js` | 完整 dashboard 逻辑：fetch + abort、KPI/insights 渲染、uPlot 图表（tooltip/resize/dblclick reset）、表格排序、drill panel、auto-refresh、theme toggle |
| `web/dist/uPlot.iife.min.js` | vendored v1.6.32（53 KB） |
| `web/dist/uPlot.min.css` | vendored（1.8 KB） |

### 测试

- `plugin/dashboard/handler_test.go`：静态资源服务（正常/404/安全头/路径穿越）
- `plugin/dashboard/insights_test.go`：insights 计算（正常/空/无异常）
- 全部通过 `go test -race`

### 关键设计决策

1. **Resource route 注册为精确路径**（host 不支持通配符）：`/index.html`、`/app.js`、`/app.css`、`/uPlot.iife.min.js`、`/uPlot.min.css`
2. **Dashboard 入口为 `/index.html`**（host 的 `normalizeResourceRoute` 拒绝 `"/"`）
3. **Resource 和 management 请求统一经 `handleManagementHandle` 分发**（host RPC adapter 对所有路由调用同一方法）
4. **Insights 60s TTL cache**：避免高频刷新时重复扫描
5. **前端 auth**：从 `sessionStorage`/`localStorage` 读取 `cpa_mgmt_key`，与 Management Center 共享登录态

## 产物

- DLL 编译成功（`bin/windows/amd64/my-cpa-stats-plugin.dll`）
- 全部测试通过（`go test -race ./plugin/...`）
- 二进制增量 ~55 KB（uPlot + 前端资产）
