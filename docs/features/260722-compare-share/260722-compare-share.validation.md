# 260722 Compare/Share 用户验证

## 验证说明

- 验证对象：Compare 报告、公开/私密 Share 快照、只读分享页、CSV 导出和 HTTPS cookie Secure 行为。
- 环境/前置条件：启动 CLIProxyAPI 宿主并加载插件；准备至少 2 个 model 或 auth 有实际请求数据；配置 `persist_path` 或 `share_path` 以启用分享；如需验证 Secure cookie，确保宿主的反向代理将 `X-Forwarded-Proto: https` 正确转发到本插件。

## 验证项

| 验证步骤 | 预期结果 | 实际结果 | 状态 | 备注/证据 |
| --- | --- | --- | --- | --- |
| 打开 dashboard，点击 `Compare`，选择 2 个 model | Compare 区域出现，选择计数为 2/6，并加载四项原始指标 |  | 待验证 |  |
| 选择第 7 个 subject | UI 拒绝选择并提示最多 6 个 |  | 待验证 |  |
| 切换 TTFT P95、TPS、成功率、总响应时间 | KPI 和排名随指标变化；低延迟指标升序，吞吐/成功率降序 |  | 待验证 |  |
| 切换 1h、6h、24h、7d、Custom | 时间范围随选择变化；Custom 校验 retention 越界并显示错误 |  | 待验证 |  |
| Hover 主图任意时间点 | 自定义 tooltip 显示时间戳、raw 值（按当前 metric 单位）和归一化值 |  | 待验证 |  |
| 点击 `Share`，保持默认公开，打开返回链接 | 新浏览器无需 Management 登录即可打开 Shared snapshot，页面隐藏刷新、选择、CSV、KPI、insights、charts、models 区域 |  | 待验证 |  |
| 创建 `Require token` 分享，先访问无 token/错误 token URL | 两种情况均返回不可区分的 404；正确 token 可打开并设置 cookie |  | 待验证 |  |
| 在 HTTPS 部署下检查浏览器 cookie 属性 | cookie 含 `Secure; HttpOnly; SameSite=Lax; Path=/v0/resource/plugins/my-cpa-stats-plugin` |  | 待验证 | 上游必须转发 `X-Forwarded-Proto: https` |
| 创建快照后产生新请求并执行 reset，再刷新分享页 | 分享内容保持创建时数据不变 |  | 待验证 |  |
| 点击 `CSV`，在 Excel 或表格工具打开 | 文件含 UTF-8 BOM；列结构正确，逗号、引号、换行不破坏字段；文件名包含 kind/metric/range/日期 |  | 待验证 |  |
| 删除分享后再次访问 | 返回 404，且与不存在/过期快照不可区分 |  | 待验证 |  |
| 配置过期时间并等待/手动模拟过期 | 过期快照返回 404；清理不会删除未过期快照 |  | 待验证 |  |

## 自动化测试结果

- `go test ./... -race`：全部通过（2026-07-23 第四轮复核验证）。覆盖项：
  - TestValidateRequest：2–6 subject 边界、retention 越界、metric 非法、trim+dedup。
  - TestBuildReportByModelAndTrend / TestBuildReportByAuth：基础报告与 auth kind。
  - TestTTP95CrossBucket：10 个 bucket × 100 样本 × TTFT=100ms 的真实 P95 应在 95–105ms 之间（B-12 回归保护）。
  - TestTTP95WeightedBucket：1000×10 vs 10×10 的请求加权场景，heavy subject P95 应 < 50ms（B-14 回归保护）。
  - TestTTP95Cap16K：1000 个满 reservoir 合并后不超过 16K 上限（B-14 follow-up）。
  - TestTTP95OrderIndependent：同一 subject 重/轻 bucket 正序与逆序合并得到相同 P95 期望（v5 B-14 加固）。
  - TestTTP95SameSubjectWeightDiff：同一 subject 内 100× 请求量差异的 bucket 加权合并（v5 B-14 加固）。
  - TestTTP95ReservoirTruncation：reservoir 截断后 P95 仍在合理范围（v5 B-14 加固）。
  - TestTTP95StrictCap16KPlusOne：16K+1 样本严格不超上限（v5 B-14 加固）。
  - TestTrend24hOver24h：trend 锁定为最近 24h vs 前 24h，与报告主范围解耦（v5 S-09）。
  - TestRangeBounds：7d vs 24h retention、custom retention 上下界。
  - TestTrendNaNGuard：零分母场景 trend=0。
  - TestMergeMetricsAcrossSubjects：多 provider/auth 合并 raw 计数后 success_rate=0.5。
  - TestCorruptSnapshot：损坏/版本不符/ID 不匹配的快照返回 ErrNotFound。
  - TestTempCleanup：`.share-*.tmp` 残片在 Cleanup 时被删除，活快照不受影响。
  - TestCreateEnforceFailureRollback：enforceMaxCount 后目录计数受约束。
  - TestShareCookieSecure / TestIsTLSRequest：cookie Secure 标志在 HTTPS 上游下启用，Path/HttpOnly/SameSite 始终保留。
  - TestResourcePaths：`/share-data` 已从静态资源路径中移除。
  - TestPublicShareNotFound / TestPublicShareTokenAndCookie / TestPublicShareNoStore：公开 handler 404/token/cookie 行为（S-06）。
  - TestNormalizeConstantSeries：常数序列归一化为 0.5（S-06）。
  - TestFrontendTooltipXSS / TestFrontendTooltipMultiSeries / TestFrontendTooltipPageCoords / TestFrontendUPlotLoadOrder / TestFrontendCSVZeroValues：前端源码模式验证（B-16～B-19）。
- `node --check plugin/dashboard/web/dist/app.js`：通过（2026-07-23 第四轮复核验证）。

## 浏览器 smoke 测试结果

使用 `scripts/smoke-test.js` mock API 服务器验证（2026-07-23 第四轮复核）：

- Dashboard 加载无 console 错误，uPlot 正确加载。
- Compare 按钮打开选择器，选择 2 个 model 后 KPI 卡（4 项）、归一化图表、排名表正确渲染。
- 排名表显示 Rank/Subject/Current value/Samples/Success/Trend/24h vs prev 24h 列，排序方向正确（TTFT P95 升序）。
- Hover 图表触发 tooltip，显示时间戳、所有 series 的 raw 值（带颜色 swatch）和归一化值。
- Share-only 模式（`share.html?id=...`）隐藏 Compare/Share/CSV/Close 按钮、选择器、KPI/insights/charts/models 区域，显示 "Shared snapshot" 标签和只读说明。
- CSV/Share 按钮在编辑模式下可用，在 share-only 模式下隐藏。

## 待跟进

- 宿主实机验证（运行 CLIProxyAPI + 实际请求流量 + 浏览器手工）仍属发布阻断项；本次未启动宿主服务。
- PNG 仍为 P2，不影响本次 MVP 验收。
