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
| reviewer v4 复核（不准入）→ v5 复核（条件准入，S-25 已修复） | 低风险类 | 完成 | reviewer | [review](../features/260722-compare-share/260722-compare-share.review.md) |
| 最终准入后填写实机 smoke 结果，等待用户书面 commit 指令 | 暂缓 | 待验证 | 用户 | [validation](../features/260722-compare-share/260722-compare-share.validation.md) |

## 执行约束

- 从当前工作区重新开始核验，不假设前几轮未完成 Task 的结果。
- 严格按 `iterator → 测试 → reviewer v4 → 修复阻塞 → 最终准入 → validation` 推进。
- 子任务以前台模式运行。
- 未经用户书面确认不得 commit。
