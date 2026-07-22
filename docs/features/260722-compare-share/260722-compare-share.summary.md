# 260722 Compare/Share 实施总结

## 背景

为 CLIProxyAPI stats plugin 增加 Compare 报告与不可变 Share 快照，采用固定公开资源 `share.html` / `share-data` 加 query 参数方案，不修改宿主动态路由契约，不改 `plugin/persist` 运行态 schema。

## 已实现

- 新增 `plugin/compare`：Compare 请求校验、1h/6h/24h/7d/custom 窗口、2–6 subject 限制、四项指标、排名方向、原始 KPI、24h 趋势和时间序列。
- 扩展 aggregator 内存 timeline，为 Compare 提供真实时间点数据；既有 persist 文件格式保持不变。
- 新增 `plugin/share`：10 位 Base58 ID、`O_EXCL` 创建、JSON 快照、过期清理、max count、大小限制、删除和 token SHA-256 constant-time 校验。
- 新增配置项和 `/stats/config` 输出：`share_enabled`、`share_path`、`share_max_count`、`share_cleanup_interval_sec`、`share_max_snapshot_bytes`、`share_available`。
- 新增 Management API：`GET /stats/compare`、`POST /stats/share`、`DELETE /stats/share`。
- 新增公开 query API：`GET /v0/resource/plugins/my-cpa-stats-plugin/share-data?id=...&token=...`；错误统一 404，成功 token 设置 HttpOnly/SameSite=Lax cookie，根据 TLS 上游标记自动追加 Secure。
- dashboard 增加 Compare 入口、Model/Auth 选择、指标/范围、归一化 uPlot 图（带 normalized/raw tooltip）、排名表、Share、CSV UTF-8 BOM/RFC4180 导出和 share-only 只读模式（隐藏 dashboard 静态区域与 CSV 按钮）。
- PNG 未实现，仍为 P2。

## 本次 review 修复（v1）

- 修复 1h/6h/24h/7d Compare timeline 选择及时间点聚合，避免以累计 bucket 代替范围结果。
- 为 TTFT 增加独立 reservoir，并以真实 TTFT P95 参与 Compare 指标。
- 多 provider/auth 按原始计数、失败数、时延和流速计数合并后再计算指标；零分母趋势保持 0。
- API key fallback 在 usage ingestion 边界改为 SHA-256 摘要；ShareStore 改为临时文件 + sync + rename，目录/快照权限收紧并增加并发互斥。
- 公开资源路径与 GET 方法精确匹配，JSON marshal 错误返回 500，share token cookie 限定 Path。
- Compare UI 增加 Custom from/to、toast live region、share-only 控件边界和四项 KPI 卡；实时请求在 share-only 模式被禁止。

## 本次 review 修复（v3）

- B-12：跨 bucket TTFT P95 改为合并各 bucket 独立 reservoir 后整体排序求 P95，避免 `sum(bucket.P95)/count` 数量级错误；上限 16K 样本，超过时按步长下采样。
- B-10：uPlot `hooks.setCursor` + 自定义 `#compare-tooltip`，hover 时显示时间戳、raw 值（按当前 metric 单位格式化）和归一化值；mouseleave 自动隐藏。
- B-08 partial + B-13：share-only 模式隐藏 `csv-btn`/KPI/insights/chart-ttft/chart-tps/chart-error/model-table/drill-panel；CSV 实现补全为 BOM + RFC4180 Blob 下载。
- B-06 partial：`cleanupLocked` 清理 `.share-*.tmp` 残片；`enforceMaxCount` 失败时回滚刚写入的 snapshot 并返回 `enforce max count` 错误。
- B-11 partial：四张 KPI 卡之上增加 `compare-kpi-note` 文案，区分"按当前 metric 排序后 top subject 的四项 KPI 卡"与"按当前 metric 的排名表"。
- S-04：根据 `X-Forwarded-Proto`/`X-Forwarded-Ssl`/`Front-End-Https` 上游头决定是否设置 `Secure`；并补充单测。
- S-06：新增 TestTTP95CrossBucket（B-12 回归）、TestRangeBounds、TestTrendNaNGuard、TestMergeMetricsAcrossSubjects、TestCorruptSnapshot、TestTempCleanup、TestCreateEnforceFailureRollback、TestShareCookieSecure、TestIsTLSRequest。
- S-07：移除 `index.html` 中残留的 `share-modal`/`modal-token`/`modal-share`/`modal-close` 结构（保留空 `<div id="share-modal">` 占位防止 stale reference）。
- 发现项 #2：从 `assetRoutes` 删除 `/share-data`，避免直发时回退到 HTML；保留 `handleManagementHandle` 中精确路径拦截。
- `go test ./...`：全部通过。
- `node --check plugin/dashboard/web/dist/app.js`：通过。
- 手工浏览器/宿主 smoke test：待用户按 validation 文档验证；本次未启动宿主服务。

## 本次 review 修复（v4）

v3 reviewer 发现的 6 个阻塞项（B-14~B-19）+ S-09~S-15 全部或部分修复。

- **B-14（加权 reservoir merge）**：`accumulator.add` 现在按 `b.TTFTReservoirCount()` 占总观测数的比例分配 reservoir 切片名额（`share = maxTTP95Samples * weight / observed`），使合并后的 P95 反映请求加权分布而非 bucket 等权分布；hard cap 强制 `len ≤ maxTTP95Samples`。新增 `TestTTP95WeightedBucket`（100× 权重差）和 `TestTTP95Cap16K`（1000 个满 reservoir 不超上限）。
- **B-15（重启后 P95）**：`persist.Restore` 不保存 reservoir，restore 后 bucket reservoir 为空 → `ttftObserved == 0` → `p95TTFT()` 返回 0 → 前端 `fmtMs` 显示 `—`。这是已知限制，与 v1 一致（timeline 是内存能力）。文档中标注。
- **B-16（tooltip XSS）**：tooltip 改用 `replaceChildren` + `createElement` + `textContent`，移除全部 `innerHTML` 拼接；label 不再参与 HTML 注入。
- **B-17（uPlot 脚本缺失）**：`index.html` 在 `app.css` 后增加 `<script src="uPlot.iife.min.js"></script>`，保证 `new uPlot(...)` 在 `app.js` 运行前可用。
- **B-18（tooltip 多 series + 定位）**：tooltip 现在列出**所有** subject 在当前时间点的 raw 与归一化值（每行带颜色 swatch），定位改用 `cursor.event.clientX/Y` 页面坐标 + `body.getBoundingClientRect()`，避免滚动错位。
- **B-19（CSV 零值）**：`point.value || ""` 与 `point.at || ""` 改为 `?? ""`，保留合法零值；无时间点时显式写一行 `(at=null, value=null)`，导出后落空。
- **S-09（B-01 trend24h）**：保留当前"前置 24h vs 当前报告范围"语义；UI 文案明确为"24h change vs previous 24h"，避免误导。仍未对齐"24h 报告 vs 24h 之前"产品定义，建议作为产品决策后处理。
- **S-10/S-12**：未在 v4 实施；enforceMaxCount 不可删除文件场景的真正失败注入需要文件系统层支持，CSV 公式注入防护依赖宿主接收的输入语义。
- **S-11（share-only 文案）**：进入 share-only 后显示 `#mode-label` "Shared snapshot · created · expires"，`#compare-count` 改为只读说明，提示用户这是锁定快照。
- **S-13（TLS 头解析）**：`isTLSRequest` 接受 `https`（区分大小写但 trimspace 后小写比较）。逗号链场景（`https,http`）未单独处理，按 S-13 留给部署文档约束。
- **S-14**：未删除空 `<div id="share-modal">`，保留以防宿主或未来集成有引用，N-07 注释简化（`returns a copy`）。
- **S-15（Point.Raw 命名）**：`Point.Raw` JSON 字段名仍为 `raw_ttft_ms`，但与 `Point.Value` 在所有 metric 下都重复。前端已不依赖 `Raw`。建议 v5 移除冗余字段或在 schema 中标注 deprecated。
- `go test ./...`：全部通过。
- `node --check plugin/dashboard/web/dist/app.js`：通过。
- 宿主实机 smoke：仍是发布阻断项，未启动宿主服务。

## 本次 review 修复（v5）

v4 reviewer 指出 B-14 与 B-15 仍需补强；本轮完成：

- **B-14 加固**：`compare.accumulator.add` 改为先收集所有 `ttftContribution`（samples + weight），调用 `weightedSample(contribs, totalWeight)` 一次性按全局权重分配名额；新算法顺序无关。新增 `TestTTP95OrderIndependent` 验证 forward/reverse 切片得到相同 P95 期望。
- **B-15 区分缺失**：`Row.TTFTObserved`（JSON `ttft_observed`）随报告下发；前端 `metricAvailable(row)` 在 `ttft_observed === 0` 时把 P95 显示为 `—`、表格 `metric_value` 列留空、CSV 中 `metric_value` 与 `p95_ttft_ms` 留空、`p95_ttft_observed` 仍输出真实计数；trend 列在缺少比较窗口时同样显示 `—`。
- KPI 卡使用 DOM API（`createElement` + `textContent`）组装，移除最后一段 `innerHTML` 模板（仅静态 className，无变量），缩小未来 XSS 回归面（N-07 + S-16）。
- `go test ./...`：全部通过。
- `node --check plugin/dashboard/web/dist/app.js`：通过。

## 变更状态

本轮代码与文档修复已完成，尚未提交 commit；按工作流仍需宿主实机验证，并等待用户书面确认后再提交。

- `plugin/persist` 仍只保存现有聚合快照；新增 timeline 是内存能力，重启后历史时间线只能从 persist 恢复为单个 bucket，不能恢复重启前完整细粒度曲线。
- 重启后 Bucket 的 TTFT reservoir 为空 → Compare 报告中 `p95_ttft_ms` 显示 `—`；这是持久化 schema 未包含 reservoir 的已知限制。
- 公开 URL 的外部 host 前缀由宿主部署环境决定，插件返回的是固定 resource path。
- PNG 导出明确不属于 MVP。
- 上游代理必须将真实协议转发到 `X-Forwarded-Proto`/`X-Forwarded-Ssl`/`Front-End-Https`，否则 Secure cookie 不会启用；本插件无法独立判断 TLS。
- enforceMaxCount 失败时的真·不可删除文件场景（如文件系统层 readonly）受 `os.Remove` 行为限制，未在测试中覆盖；当前代码路径已确保写入的 snapshot 在 enforce 失败时被回滚。
