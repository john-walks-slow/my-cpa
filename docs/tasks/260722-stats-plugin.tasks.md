# 模型速度统计插件 — 任务跟踪

> Idea：为 CLIProxyAPI（https://github.com/router-for-me/CLIProxyAPI ）开发一个 Go 编写的插件，利用 plugin interceptor 机制采集模型性能指标（TTFB、总响应时间、tokens/sec、token 数），按模型/账号维度聚合，并通过 HTTP 查询接口暴露统计结果。

| # | 任务 | 类型 | 状态 | 分派至 | 产出文档 |
|---|------|------|------|--------|----------|
| 1 | 调研 CLIProxyAPI 插件 ABI + 设计插件架构 | 调研类 + 新需求类 | 完成 | planner | `docs/features/260722-stats-plugin/260722-stats-plugin.research.md`、`260722-stats-plugin.plan.md` |
| 2 | 实现插件并交付可加载二进制 | 新需求类 | 完成 | iterator | `docs/features/260722-stats-plugin/260722-stats-plugin.summary.md`、`260722-stats-plugin.validation.md` |
| 3 | Code review | 新需求类 | 完成 | reviewer | `docs/features/260722-stats-plugin/260722-stats-plugin.review.md` |
| 4 | 修复 review 阻塞项 | 低风险类 | 完成 | iterator | — |
| 5 | ~~健康评分与告警机制~~ | — | 已取消 | — | 用户取消，方向转向 dashboard |
| 6 | Dashboard — 调研+设计 | 调研类 + 新需求类 | 完成 | planner | `docs/features/260722-dashboard/260722-dashboard.research.md`、`260722-dashboard.plan.md` |
| 7 | Dashboard — 实施 | 新需求类 | 完成 | iterator | `docs/features/260722-dashboard/260722-dashboard.summary.md`、`260722-dashboard.validation.md` |
| 8 | Dashboard — review | 新需求类 | 完成 | reviewer | `docs/features/260722-dashboard/260722-dashboard.review.md` |
| 9 | Dashboard — 修复 review 建议项 | 低风险类 | 完成 | iterator | — |
| 10 | Model 对比与可分享快照 — 调研+设计 | 调研类 + 新需求类 | 完成 | planner | `docs/features/260722-compare-share/260722-compare-share.research.md`、`260722-compare-share.plan.md` |
| 11 | Model 对比与可分享快照 — 实施 | 新需求类 | 进行中 | iterator | `docs/features/260722-compare-share/260722-compare-share.summary.md`、`260722-compare-share.validation.md` |
| 12 | Model 对比与可分享快照 — review | 新需求类 | 待开始 | reviewer | `docs/features/260722-compare-share/260722-compare-share.review.md` |

依赖：1 → 2。