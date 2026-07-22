# my-cpa AGENTS.md

## 目标

CLIProxyAPI c-shared 插件：模型速度统计、内嵌 dashboard、compare & share。

## 地图

| 模块 | 职责 |
|------|------|
| `plugin/` | 主插件包（package main, c-shared 入口、生命周期、路由） |
| `plugin/aggregator/` | 内存聚合引擎（时间窗口 × series key，reservoir sampling） |
| `plugin/compare/` | 对比报告生成 |
| `plugin/config/` | YAML 配置解析 |
| `plugin/dashboard/web/dist/` | 前端静态文件（vendored uPlot + vanilla JS/CSS，go:embed 嵌入） |
| `plugin/persist/` | JSON snapshot 持久化 |
| `plugin/share/` | 分享快照（不可变、token 保护、过期清理） |
| `cmd/build/main.go` | 跨平台构建脚本 |
| `scripts/smoke-test.js` | 开发用静态服务器 |

## 开发与调试

```bash
# 构建（单平台，需 CGO_ENABLED=1）
make build
# 或手动：go build -buildmode=c-shared -o bin/my-cpa-stats-plugin.dll ./plugin

# 全平台构建
go run cmd/build/main.go

# 测试
make test
# 等价于：go test ./... -race && node --check plugin/dashboard/web/dist/app.js

# Lint
make lint
```

## 规范

- **插件 ABI**：`plugin/plugin.go` 中 `//export` 符号不可改名，否则 host 无法加载。
- **前端无构建链**：直接编辑 `plugin/dashboard/web/dist/` 下的单文件 HTML/JS/CSS，修改后 `node --check` 验证。
- **go:embed**：新增前端文件需确认 `plugin/dashboard/embed.go` 中 embed 路径匹配。
- **Dashboard 路径固定**：`/v0/resource/plugins/my-cpa-stats-plugin/`，不可更改。

## 关键设计决策

- **双能力注册**：UsagePlugin（采集）+ ManagementAPI（查询/管理），零侵入不启用 interceptor。
- **内存聚合 + JSON persist**：重启后从 snapshot 恢复，persist 可选。
- **Reservoir sampling**：P50/P95 分位数，容量 1024，兼顾精度与内存。
- **Compare & Share**：对比报告实时生成；share 创建不可变快照，支持 token 保护和过期。
