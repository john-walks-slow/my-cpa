# 260722-dashboard · Validation

## 自动化测试

```bash
go test -race ./plugin/...
```

预期：全部通过（aggregator / config / dashboard / persist）。

## 编译验证

```bash
go build -buildmode=c-shared -o bin/windows/amd64/my-cpa-stats-plugin.dll ./plugin
```

预期：成功生成 DLL，无编译错误。

## 浏览器手动验证

### 前置条件

1. 启动 CLIProxyAPI（dev 模式或正式部署）
2. 加载本插件（`plugins/<GOOS>/<GOARCH>/my-cpa-stats-plugin.{dll,so,dylib}`）
3. 配置 `dashboard_enabled: true`（默认）
4. 触发若干真实模型请求（至少 10 次，覆盖 2+ 模型）

### 验证清单

| # | 场景 | 操作 | 预期结果 | 实际结果 | 状态 |
|---|------|------|----------|----------|------|
| 1 | Dashboard 入口可见 | 访问 `http://<host>/v0/resource/plugins/my-cpa-stats-plugin/index.html` | 页面加载，显示 "Stats Dashboard" 标题 | | ☐ |
| 2 | KPI 卡片渲染 | 观察顶部 4 个卡片 | 显示 Requests / P50 TTFT / Avg TPS / Error Rate，数值非 "—" | | ☐ |
| 3 | Insights 卡片渲染 | 观察 KPI 下方 4 个卡片 | 显示 Fastest / Slowest / Most Stable / Last Anomaly，含模型名和数值 | | ☐ |
| 4 | 趋势图渲染 | 观察 3 个图表区域 | TTFT P50/P95、TPS、Error Rate 图表显示时间序列曲线 | | ☐ |
| 5 | 时间范围切换 | 点击 "15m" / "1h" / "6h" / "24h" / "7d" | 图表和表格数据刷新，时间轴范围变化 | | ☐ |
| 6 | 图表交互 - tooltip | 鼠标悬停图表曲线 | 显示 tooltip，含时间和各 series 数值 | | ☐ |
| 7 | 图表交互 - 缩放 | 鼠标拖拽选择时间区间 | 图表缩放到选中区间 | | ☐ |
| 8 | 图表交互 - 重置 | 双击图表 | 缩放重置到完整时间范围 | | ☐ |
| 9 | 模型表格排序 | 点击表头 "Reqs" / "P50" / "P95" / "TPS" / "Err%" | 表格按该列升序/降序排列 | | ☐ |
| 10 | 下钻面板 | 点击表格中任一模型行 | 右侧滑出面板，显示该模型的 auth 级别明细 | | ☐ |
| 11 | 下钻面板关闭 | 点击面板右上角 "×" 或按 Esc | 面板关闭 | | ☐ |
| 12 | 自动刷新 | 选择 "30s" 或 "1m" | 每 30s/1m 自动刷新数据（观察 KPI 数值变化） | | ☐ |
| 13 | 手动刷新 | 点击刷新按钮 "↻" | 立即刷新数据 | | ☐ |
| 14 | 暗色模式 | 点击主题切换按钮 "◐" | 页面切换为暗色主题，再次点击切回亮色 | | ☐ |
| 15 | 移动端响应式 | 浏览器 DevTools 切换到移动设备视口（< 768px） | 布局变为单列，KPI 卡片纵向堆叠，图表全宽 | | ☐ |
| 16 | 空数据状态 | 清空聚合数据（`POST /v0/management/stats/reset`）后刷新页面 | 显示 "No data yet" 提示，图表为空 | | ☐ |
| 17 | 鉴权失败状态 | 清除 `sessionStorage`/`localStorage` 中的 `cpa_mgmt_key` 后刷新 | 页面顶部显示红色 banner "Management key missing or expired" | | ☐ |
| 18 | 资源路径 404 | 访问 `/v0/resource/plugins/my-cpa-stats-plugin/secret.txt` | 返回 404 | | ☐ |
| 19 | 路径穿越防御 | 访问 `/v0/resource/plugins/my-cpa-stats-plugin/../../etc/passwd` | 返回 404 | | ☐ |
| 20 | CSP 头验证 | DevTools Network 面板查看 `/index.html` 响应头 | 包含 `Content-Security-Policy: ... script-src 'self' ...` | | ☐ |

### 浏览器兼容性

在以下浏览器中重复验证 #1-#14：

- [ ] Chrome / Edge（最新版）
- [ ] Firefox（最新版）
- [ ] Safari（最新版，macOS/iOS）

### 性能验证

| 场景 | 操作 | 预期 |
|------|------|------|
| 首屏加载 | 刷新页面 | < 2s（本地网络） |
| 大数据量 | 触发 1000+ 请求后刷新 | 图表渲染 < 500ms，无卡顿 |
| 长时间运行 | 保持页面打开 10min+（自动刷新 30s） | 无内存泄漏，FPS 稳定 |

## 配置验证

### 关闭 dashboard

```yaml
plugins:
  configs:
    my-cpa-stats-plugin:
      dashboard_enabled: false
```

重启插件后：
- 访问 `/v0/resource/plugins/my-cpa-stats-plugin/index.html` 返回 404
- `/v0/management/stats/insights` 返回 404（路由未注册）

### 重新启用

```yaml
dashboard_enabled: true
```

重启后 dashboard 恢复可访问。

## 回归验证

确保现有 stats API 不受影响：

```bash
curl -H "Authorization: Bearer <key>" http://<host>/v0/management/stats/overview
curl -H "Authorization: Bearer <key>" http://<host>/v0/management/stats/series?window=1m
curl -H "Authorization: Bearer <key>" http://<host>/v0/management/stats/by-model?window=5m
```

预期：返回 JSON，状态码 200。

## 验证结论

验证人：_______________  
验证日期：_______________  
总体结论：☐ 通过 ☐ 不通过  
备注：
