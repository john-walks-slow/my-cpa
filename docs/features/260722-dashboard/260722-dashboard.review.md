# 检视报告

## 概要

检视范围：stats 插件新增 dashboard 功能（后端 Go + 前端 vanilla JS）及 `/stats/*` 路由 Menu 字段 bug 修复。整体实现质量良好，架构清晰，与计划对齐度高。安全加固（CSP、白名单、textContent）到位。存在一个前端超时机制失效的逻辑 bug 和一处违反计划 XSS 原则的 innerHTML 使用。

## 需求对齐

- Menu 字段移除：已完成，所有 `/stats/*` GET 路由不再携带 `Menu` 字段。
- Dashboard 静态资源服务：已实现，白名单 + 安全头 + CSP 均到位。
- `/stats/insights` endpoint：已实现，O(N) 扫描 + 60s TTL cache。
- `by-auth` 新增 `model` 过滤：已实现（匹配 model 或 alias）。
- `DashboardEnabled *bool` 配置：已实现，默认 true，关闭时不注册 Resources。
- 前端功能：KPI 卡片、趋势图、模型表、下钻面板、自动刷新、暗色模式、响应式均已实现。
- README 已同步更新。

与计划的偏差（均可接受）：
- 计划中 `static.go` 独立文件合并进了 `handler.go`。
- 计划中 resource 入口为 `/`，实现为 `/index.html`（README 已对齐）。
- 计划中 insights cache 用 `sync.RWMutex`，实现用 `sync.Mutex`（当前规模无影响）。

## 阻塞问题

无。

## 建议修改

| ID | 位置 | 问题 | 建议 |
| --- | ---- | ---- | ---- |
| S1 | `plugin/dashboard/web/dist/app.js:29-44` | `apiFetch` 的超时机制在有父级 signal 时完全失效。`combinedSignal = signal \|\| ctrl.signal` 导致当 `fetchAll` 传入 signal 时，fetch 使用的是父级 signal 而非超时 control 的 signal，`setTimeout(() => ctrl.abort(), 8000)` 永远不会中断请求。 | 使用 `AbortSignal.any([signal, ctrl.signal])` 合并两个 signal（或在不支持 `any` 的环境中监听父级 signal 的 abort 事件来触发 ctrl.abort）。 |
| S2 | `plugin/dashboard/web/dist/app.js:263-272` | tooltip 通过 `tip.innerHTML = html` 插入包含 model 名称（用户可控数据）的 HTML。虽然 CSP `script-src 'self'` 阻止了脚本执行，但仍可注入可见 HTML 元素干扰 UI，且违反计划 §8.5 "严禁 innerHTML 插入动态字符串" 的原则。 | 改用 DOM API 构建 tooltip 内容（`createElement` + `textContent`），或对 model 名做 HTML entity 转义后再拼接。 |
| S3 | `plugin/dashboard/web/dist/app.js:283-287` | `renderCharts` 在无 series 数据时切换 `#table-empty` 的可见性，但该元素语义上属于 table section。当 series 为空但 byModel 非空时（理论上不会，但逻辑耦合不清晰），会产生状态冲突。 | 为 chart section 增加独立的空状态元素，或统一由一个 `renderEmptyState(hasData)` 函数管理所有空状态。 |

## 非阻塞问题

| ID | 位置 | 问题 | 建议 |
| --- | ---- | ---- | ---- |
| N1 | `plugin/capabilities.go:35` | `dashboard_enabled` 行缩进与上方字段不一致（少一个 tab）。 | 对齐缩进。 |
| N2 | `plugin/management_handler.go:73` | resource 路径前缀 `"/v0/resource/plugins/my-cpa-stats-plugin"` 硬编码在 management_handler.go 和 dashboard/handler.go 两处。 | 可提取为 dashboard 包导出的 const，management_handler 引用之，减少重复。当前规模下不紧急。 |
| N3 | `plugin/dashboard/web/dist/app.js:341-343` | 双击重置缩放使用 `setScale("x", {min, max})` 硬编码为当前数据范围。若数据刷新后范围变化，双击恢复的是旧范围。 | 改为 `charts[panelId].setScale("x", null)` 让 uPlot 自动适配当前数据范围（计划 §8.3 也写的是 `setScale('x', null)`）。 |
| N4 | `plugin/dashboard/web/dist/app.js:499-506` | 表头排序点击触发 `fetchAll()` 重新请求网络数据，但排序仅是客户端行为，无需重新 fetch。 | 排序点击后仅调用 `renderTable(lastByModel)` 重新渲染即可，避免不必要的网络请求。 |
| N5 | `plugin/management_handler.go:297-321` | `statsInsights` 在 `insightsMu.Lock()` 下执行完整的 O(N) 计算。高并发时所有请求串行等待。 | 当前规模（N < 50K，计算 < 5ms）可接受。若后续 N 增长，可改为 double-check locking 或 `sync.Once` 模式。记录备忘。 |

## 准入结论

**结论**：`条件准入`

**说明**：无阻塞问题。S1（超时失效）和 S2（innerHTML 注入用户数据）建议在合并前修复，均为小改动；S3 及非阻塞项可在后续迭代处理。
