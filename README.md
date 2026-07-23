# my-cpa-stats-plugin

模型速度统计插件 for CLIProxyAPI。

采集 TTFT / 总耗时 / token / 速率，按 (provider, model, alias, auth_id) × 时间窗口聚合，通过 `/v0/management/stats/*` 暴露查询 API。

## 特性

- **UsagePlugin + ManagementAPI** 双能力，零侵入（不启用 interceptor）
- 内存聚合 + 可选 JSON snapshot 持久化
- 时间窗口：1m / 5m / 15m / 1h / 24h
- 分位数：P50 / P95（reservoir sampling）
- 流式输出速率：tokens/sec
- 基数限制：LRU 淘汰（默认 50,000 series）
- **内嵌 Dashboard**：`/v0/resource/plugins/my-cpa-stats-plugin/index.html`，趋势图 + KPI + 模型对比 + 下钻（uPlot，vendored，无构建链）
- **Compare & Share**：多模型/多账号对比报告，支持创建不可变分享快照（token 保护、可设过期）

## 构建

```bash
# 单平台（当前系统）
go build -buildmode=c-shared -o my-cpa-stats-plugin.so ./plugin

# 跨平台
go run cmd/build/main.go
```

产物：`bin/<GOOS>/<GOARCH>/my-cpa-stats-plugin.{so,dylib,dll}`

## 安装

将二进制放入 CLIProxyAPI 的 `plugins/<GOOS>/<GOARCH>/` 目录，host 启动时自动加载。

## Install via Plugin Store

通过 CLIProxyAPI 的第三方插件源一键安装，无需手动传文件。

1. 在 `config.yaml` 添加：

   ```yaml
   plugins:
     store-sources:
       - "https://raw.githubusercontent.com/john-walks-slow/my-cpa/master/registry.json"
   ```

2. 重启 cpa，访问 Management Center → Plugin Store，或调用：

   ```bash
   curl http://localhost:8317/v0/management/plugin-store
   ```

3. 在列表中点击 **Install**，cpa 会从 GitHub Release 下载对应平台的 zip 并解压到 `plugins/` 目录。

4. 在 `config.yaml` 中启用并配置（见下文「配置」一节）。

## 配置

在 `config.yaml` 中（完整示例见 [`plugin/docs/config.example.yaml`](plugin/docs/config.example.yaml)）：

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    my-cpa-stats-plugin:
      enabled: true
      retention_minutes: 1440          # 24h
      persist_path: "stats.json"        # 留空则不落盘
      persist_interval_sec: 30
      cardinality_limit: 50000
      dashboard_enabled: true           # 内嵌 dashboard 开关，默认 true
```

## Dashboard

访问 `http://<host>/v0/resource/plugins/my-cpa-stats-plugin/index.html` 查看内嵌 dashboard。

<!-- TODO: add screenshot -->

功能：
- **KPI 卡片**：总请求数、P50 TTFT、平均 TPS、错误率
- **趋势图**：TTFT P50/P95、TPS、错误率时间序列（15m / 1h / 6h / 24h / 7d 可切）
- **模型对比表**：按请求数、P50、P95、TPS、错误率排序
- **下钻面板**：点击模型行查看该模型的 auth 级别明细
- **自动刷新**：off / 30s / 1m / 5m
- **暗色模式**：跟随系统或手动切换

Dashboard 依赖 Management Center 的登录态（`sessionStorage` / `localStorage` 中的 `cpa_mgmt_key`）。若未登录，页面顶部会显示红色 banner 提示。

关闭 dashboard：在配置中设置 `dashboard_enabled: false`，重启插件后不再注册 resource routes。

## API

所有路由需 management key 鉴权（同 `/v0/management/api-keys`）。

| Method | Path | 描述 |
|--------|------|------|
| GET | `/v0/management/stats/overview` | 顶层聚合统计 |
| GET | `/v0/management/stats/series?window=1m&model=gpt-5.5&auth=auth-1` | 时间序列查询 |
| GET | `/v0/management/stats/by-model?window=5m` | 按模型聚合 |
| GET | `/v0/management/stats/by-auth?window=1h` | 按账号聚合 |
| GET | `/v0/management/stats/insights` | Dashboard KPI 卡片（server-side 聚合，60s cache） |
| GET | `/v0/management/stats/keys` | 列出已知 series |
| POST | `/v0/management/stats/reset` | 清空内存聚合 |
| GET | `/v0/management/stats/config` | 当前生效配置 |
| GET | `/v0/management/stats/compare?kind=model&id=a,b` | 对比报告（kind: model/auth/provider） |
| POST | `/v0/management/stats/share` | 创建不可变分享快照 |
| DELETE | `/v0/management/stats/share?id=<id>` | 删除分享快照 |
| GET | `/v0/resource/plugins/my-cpa-stats-plugin/share-data?id=<id>` | 公开访问分享快照（无需 management key） |

示例：

```bash
curl -H "Authorization: Bearer <management-key>" \
  http://localhost:8317/v0/management/stats/series?window=1m
```

## 已知限制

- **零 token 成功请求不计数**：host 的 `usage.PublishRecord` 在「成功且全部 token 字段为 0」时跳过派发（见 `internal/runtime/executor/...usage_helpers.go`）。失败请求始终被采集。
- 分位数使用 reservoir sampling（容量 1024），极端分布下可能有误差。

## 测试

```bash
make test
# 等价于：
go test ./... -race
node --check plugin/dashboard/web/dist/app.js
```

## License

MIT
