# 260723 Compare/Share 第四轮修复任务

| 任务 | 类型 | 状态 | 分派至 | 产出文档 |
| ---- | ---- | ---- | ------ | -------- |
| 修复 B-14 TTFT 跨 bucket 请求加权合并（v5 重做 deferred-merge + 顺序无关测试） | 低风险类 | 完成 | iterator | [summary](../features/260722-compare-share/260722-compare-share.summary.md) |
| 修复 B-15 TTFT 缺失标记（ttft_observed 字段 + 前端/CSV 区分缺失与零值） | 低风险类 | 完成 | iterator | [summary](../features/260722-compare-share/260722-compare-share.summary.md) |
| 核验并完善 B-16～B-19 前端安全、资源加载、tooltip、CSV 修复及测试 | 低风险类 | 完成 | iterator | [validation](../features/260722-compare-share/260722-compare-share.validation.md) |
| 按选项 A 将 trend 统一为最近 24h vs 前 24h，并改名 trend_24h_over_24h | 低风险类 | 完成 | iterator | [summary](../features/260722-compare-share/260722-compare-share.summary.md) |
| 补 S-06：重启 P95、公开 handler 404/token/cookie、常数归一化、tooltip/CSV 测试 | 低风险类 | 完成 | iterator | [validation](../features/260722-compare-share/260722-compare-share.validation.md) |
| S-25：CSV p95_ttft_ms 列在 ttft_observed===0 时写空字符串 | 低风险类 | 完成 | iterator | [review](../features/260722-compare-share/260722-compare-share.review.md) |
| 运行完整自动化测试（go test -race + node --check） | 低风险类 | 完成 | iterator | [validation](../features/260722-compare-share/260722-compare-share.validation.md) |
| 浏览器 smoke 测试（mock API + 真实前端交互） | 低风险类 | 完成 | iterator | [validation](../features/260722-compare-share/260722-compare-share.validation.md) |
| reviewer v4 复核（不准入）→ v5 复核（条件准入，S-25 已修复） | 低风险类 | 完成 | reviewer | [review](../features/260722-compare-share/260722-compare-share.review.md) |
| 最终准入后填写实机 smoke 结果，等待用户书面 commit 指令 | 暂缓 | 待验证 | 用户 | [validation](../features/260722-compare-share/260722-compare-share.validation.md) |

## 执行约束

- 从当前工作区重新开始核验，不假设前几轮未完成 Task 的结果。
- 严格按 `iterator → 测试 → reviewer v4 → 修复阻塞 → 最终准入 → validation` 推进。
- 子任务以前台模式运行。
- 未经用户书面确认不得 commit。

## 第四轮复核记录（2026-07-23）

本轮从当前工作区重新核验并完成所有阻塞项：

- **B-14**：确认 `weightedSample` 算法顺序无关、严格 `len ≤ maxTTP95Samples`；4 项测试全部通过（100× 权重差、正/逆序、reservoir 截断、16K+1 边界）。
- **B-15**：确认 `ttft_observed` 字段在 JSON 报告中正确输出；前端 `metricAvailable` 在 `ttft_observed === 0` 时显示 `—`；CSV 中 `p95_ttft_ms` 列在缺失时留空。
- **B-16～B-19**：确认 tooltip 使用 DOM API（无 innerHTML）、uPlot 在 app.js 前加载、tooltip 列出所有 series 并使用 clientX/Y 定位、CSV 使用 `??` 保留零值。
- **S-09 选项 A**：确认 `trend_24h_over_24h` 字段名、`computeTrend24hOver24h` 始终使用最近 24h vs 前 24h、前端表头为 "24h vs prev 24h"。
- **S-06**：确认公开 handler 404/token/cookie 测试、常数归一化测试、tooltip/CSV 源码模式测试全部通过。
- **浏览器 smoke**：使用 `scripts/smoke-test.js` mock API 服务器验证 dashboard 加载、Compare 选择、KPI/图表/排名表渲染、tooltip 多 series 显示、share-only 模式控件隐藏。
- `go test ./...`：全部通过。
- `node --check plugin/dashboard/web/dist/app.js`：通过。

**结论**：所有阻塞项已修复并通过自动化测试与浏览器 smoke 验证，等待用户实机验证后书面确认 commit。
