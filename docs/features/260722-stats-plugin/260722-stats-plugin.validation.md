# 260722-stats-plugin · Validation

## 自动化验证（已通过）

- [x] `go build -buildmode=c-shared -o bin/windows/amd64/my-cpa-stats-plugin.dll ./plugin` 编译成功
- [x] `go test -race ./plugin/aggregator ./plugin/persist ./plugin/config` 全部通过
- [x] `go vet` 无警告

## 用户实机验证

> 以下场景需要 CLIProxyAPI dev 实例 + 本插件加载后验证。

### 1. 插件加载

| 步骤 | 预期 | 实际 | 状态 |
|------|------|------|------|
| 将 DLL 放入 `plugins/windows/amd64/` | host 日志显示插件加载成功 | | |
| config.yaml 中 `my-cpa-stats-plugin.enabled: true` | 无报错 | | |

### 2. 数据采集

| 步骤 | 预期 | 实际 | 状态 |
|------|------|------|------|
| 发起 1 次 OpenAI 流式请求 | `/stats/overview` 的 `total_requests` ≥ 1 | | |
| 发起 1 次失败请求（无效 model） | `failed_count` 增加 | | |

### 3. 查询 API

| 步骤 | 预期 | 实际 | 状态 |
|------|------|------|------|
| `GET /v0/management/stats/series?window=1m` | 返回 JSON 数组，含 avg_latency_ms、avg_stream_rate_tps | | |
| `GET /v0/management/stats/by-model?window=5m` | 按模型聚合 | | |
| `GET /v0/management/stats/keys` | 列出 series key | | |
| `POST /v0/management/stats/reset` | 返回 `{"status":"reset"}`，再查 overview 为 0 | | |

### 4. 持久化（可选）

| 步骤 | 预期 | 实际 | 状态 |
|------|------|------|------|
| 配置 `persist_path: "stats.json"` | 30s 后出现 stats.json 文件 | | |
| 重启 host | 数据恢复，overview 非零 | | |

## 备注

- 零 token 成功请求不计数是已知限制（host 侧行为）
- 分位数为 reservoir sampling 近似值
