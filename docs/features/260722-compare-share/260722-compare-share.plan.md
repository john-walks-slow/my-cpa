# 260722 Compare/Share 实施计划

## 1. 目标与边界

为现有 stats dashboard 增加“比较哪个 model/auth 真正好用”的报告工作流，并支持不可变、可公开访问的分享快照。

范围内：

- 顶部 Compare 入口；选择 2–6 个 model 或 auth。
- 时间窗 1h / 6h / 24h / 7d / 自定义；指标 TTFT P95、TPS、成功率、总响应时间。
- 并排 KPI、单张归一化主图、按当前指标排序的排名表和 24h 趋势箭头。
- 生成 JSON 快照并分享；公开访问，或每个快照单独 require token。
- 前端 CSV Blob 导出。

明确不做：数据库、跨实例分享、复杂权限、模板市场、双 Y 轴默认模式、PNG MVP。

## 2. 选定的整体方案

报告生成在插件后端完成，保证“生成时锁定”：请求创建分享时，后端读取 `aggregator.Snapshot()`，按请求的 subjects、range、metric 聚合为完整报告数据，并立即写入单个 JSON 文件。之后只读报告不再访问实时 aggregator。

Compare 编辑视图可以直接调用新增 compare 数据 endpoint 获取当前数据；Share 调用 create endpoint 生成同样 schema 的不可变快照。只读页面通过相同 `index.html/app.js/app.css/uPlot` 加 `?share=<short_id>` 启动，检测 share 模式后隐藏刷新、配置和编辑控件。

由于宿主当前只支持精确 Management route，动态分享 URL 不能注册为 `/stats/share/{id}`。注册固定精确路由 `GET /stats/share`，由 query `?id=<short_id>&token=<token>` 读取快照；同时注册公开的精确 ResourceRoute `/share.html` 作为无 management key 的只读入口，页面再调用公开的 `GET /stats/share?id=...` 仍会遇到宿主 Management 鉴权，因此实际公开读取也需要一个公开 ResourceRoute API 路由，例如 `/share-data`，其 query 包含短 ID。若宿主无法把 ResourceRoute API 作为 JSON 动态路由暴露，必须在宿主增加能力；本插件不能绕过宿主鉴权。计划默认采用宿主新增“public resource handler”支持，或将 `/v0/management/stats/share/{short_id}` 作为宿主专门允许的公开插件路由，实施前需与宿主 API owner 对齐。

## 3. 快照存储结构

目录：

```text
<share_root>/shares/<short_id>.json
```

`share_root` 解析顺序：`share_path`（若配置）→ `filepath.Dir(persist_path)`（若 persist_path 配置）→ 无目录则 Share disabled。目录创建 `0755`，文件 `0644`；若部署环境要求更严，可配置为 `0700/0600`。

文件 schema：

```json
{
  "schema_version": 1,
  "id": "7Gk3pQw9Xa",
  "created_at": "2026-07-22T07:30:00Z",
  "expires_at": "2026-07-29T07:30:00Z",
  "require_token": true,
  "token_sha256": "<64 hex chars>",
  "title": "Model comparison",
  "range": {"preset":"24h","from":"...","to":"..."},
  "metric": "p95_ttft_ms",
  "subjects": [
    {"kind":"model","id":"provider|model|alias","label":"..."}
  ],
  "rows": [
    {
      "subject":"...",
      "label":"...",
      "count": 123,
      "success_rate": 0.98,
      "p95_ttft_ms": 210,
      "avg_stream_rate_tps": 85.2,
      "avg_latency_ms": 3200,
      "trend_24h": 0.12,
      "series": [{"at":"...","value":210}]
    }
  ]
}
```

不要把 management key、原始 token 明文或未脱敏 API key 写入快照。`subjects` label 只使用现有 model/auth 标识；若 auth 标识可能包含秘密，后端必须在快照前脱敏。

写入：随机生成 ID → `O_EXCL` 检查冲突 → JSON marshal → `<id>.tmp` → rename。启动清理一次，每小时清理一次；过期文件删除。`share_max_count > 0` 时创建后按 created_at 删除最旧文件。读取时校验 schema、ID 文件名一致性、expires_at 和大小上限。

## 4. 短 ID 与访问鉴权

- `crypto/rand` rejection sampling。
- Base58 alphabet：`123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz`。
- 长度 10；冲突重试。
- 默认公开：`require_token=false`，只需要短 ID。
- 私密分享：生成至少 128 bit 随机 token，仅保存 SHA-256；create 响应一次性返回 `url`（含 token）和不含 token 的 `short_url`。
- 访问 token 先读 query，再读 `cpa_share_token` HttpOnly/SameSite=Lax cookie；query 校验成功后设置 cookie 并 302 到无 query token URL（若宿主 handler 不便 302，则前端立即 replaceState，MVP 可接受但 token 会暂留地址栏）。
- token 比较使用 hash 后 constant-time compare；错误统一返回 404，避免泄露“ID 存在但 token 错误”。
- “永不过期”仍受 `share_max_count` 约束；默认公开链接不等于可修改，快照全程只读。

## 5. 配置 schema

在 `plugin/config/config.go` 增加：

```yaml
share_enabled: true
share_path: ""             # 默认取 dirname(persist_path)
share_max_count: 1000       # 0 表示不限制
share_cleanup_interval_sec: 3600
share_max_snapshot_bytes: 5242880
```

默认 `share_enabled` 可设为 true，但没有可解析 share root 时运行时禁用并返回明确错误；不要静默把快照写入当前工作目录。`share_path` 不承担访问 secret；MVP 不新增 `share_secret`，未来 HMAC 作为独立 P2。

`/stats/config` 返回上述有效配置和 `share_available`，不返回任何 token 或 secret。

## 6. 后端 endpoint 清单

所有 endpoint 返回 `Content-Type: application/json`，错误统一 `{error: string}`。

### Management-authenticated

1. `GET /v0/management/stats/compare`
   - query：`kind=model|auth`、`ids`（重复参数或逗号列表，2–6）、`range`、`from`、`to`、`metric`。
   - 返回当前实时报告 schema；只读，不修改状态。
2. `POST /v0/management/stats/share`
   - body：Compare 请求 + `require_token` + `expires_in=24h|7d|30d|never`。
   - 返回 `id`、`short_url`、可选一次性 `url`、`expires_at`。
3. `DELETE /v0/management/stats/share?id=<id>`
   - 删除本实例快照；用于创建者撤销，若宿主只接受 exact route 则使用 `POST /stats/share/delete`。
4. `GET /v0/management/stats/config` 扩展分享字段。

### Public resource/API

需宿主支持公开插件动态资源路由后提供：

1. `GET /v0/resource/plugins/my-cpa-stats-plugin/share.html`：复用 dashboard HTML，进入只读模式。
2. `GET /v0/resource/plugins/my-cpa-stats-plugin/share-data?id=<id>&token=<token>`：读取不可变快照；公开 JSON，不访问实时 aggregator。

用户要求的 `/v0/management/stats/share/{short_id}` 可作为对外 canonical URL，但当前宿主 route contract 不支持动态 path。若宿主后续支持动态插件路由，应把该路径作为 301/302 入口，内部仍由同一 ShareStore 读取；在未支持前不得在插件中伪造前缀匹配。

## 7. 前端区域与现有 dashboard 修改

### 顶部 toolbar

- 增加 `Compare` 主按钮和当前模式标识。
- Compare 编辑视图显示 `Share`、`CSV`，PNG 按钮置为 P2 不出现。
- share-only 模式显示“Shared snapshot”、创建时间、过期时间；隐藏 auto refresh、refresh、选择器、theme 可保留但不影响数据，隐藏 drill 编辑行为。

### Compare 区域

- 选择器按 kind 切换 Model/Auth；可搜索、全选不提供，限制 2–6 个并显示计数。
- 时间窗 preset + 自定义起止时间；自定义范围不得超过 aggregator retention，超出显示错误。
- 指标 selector：P95 TTFT、TPS、成功率、总响应时间；每次切换重排 KPI 和排名。
- 四张并排指标卡；卡片显示原始单位和样本数。
- 主图复用 uPlot：X 轴时间，序列值 min-max 到 [0,1]，tooltip 显示 normalized 与 raw；数据不足时显示空态。
- 排名表列：Rank、subject、当前值、样本、成功率、趋势箭头、24h 变化；低延迟指标升序，高吞吐/成功率降序。

### 现有 dashboard

- 默认首页保留当前 KPI、趋势、Models、drill-down，顶部进入 Compare，不破坏已有 API 调用。
- 把 `apiFetch` 拆为 authenticated fetch 与 public share fetch；share mode 禁止访问 `/overview`、`/insights` 等实时 endpoint。
- 抽取 range/metric formatting、uPlot resize、CSV escaping 为可复用函数，避免复制 dashboard 渲染逻辑。
- 静态资源继续使用 `web/dist/` 和 vendored uPlot；新增 `share.html` 是否独立文件取决于宿主资源路由，优先单 index 根据 query 切换，避免重复嵌入。

## 8. CSV 与 PNG

CSV MVP 前端 Blob：导出当前报告的原始行和每个时间点字段，UTF-8 BOM、RFC 4180 转义、`createObjectURL` 后下载并 revoke。导出文件名包含 kind、metric、range 和 UTC 日期。分享页也允许 CSV，因为数据已经锁定。

PNG 不进入 MVP，列为 P2：vendored 固定版本 html2canvas，等待 `document.fonts.ready`，复制并展开报告容器，临时禁用动画，限制最大画布面积，导出后恢复布局。uPlot canvas 与表格需单独验证；失败应提示用户使用浏览器截图/打印，不影响报告和分享。

## 9. 任务拆分（iterator 友好）

### Iterator 1：抽取 Compare 数据模型

- 新建 `plugin/compare`，定义请求、报告、指标方向、窗口解析和聚合纯函数。
- 基于现有 `aggregator.Snapshot()` 输出 model/auth rows、series 和 24h trend。
- 补齐 2–6、非法 metric、时间范围、空数据、retention 边界测试。

### Iterator 2：ShareStore

- 新建 `plugin/share`，实现目录解析、10 字符 Base58、atomic write/read/delete、过期清理、max count。
- 测试重启读取、过期、文件损坏、ID 冲突、token hash、权限错误。
- 不修改现有 `persist.Store` schema。

### Iterator 3：Management API

- 注册 compare/create/delete/config 路由。
- 添加 JSON body/query 解析、错误状态、快照生成时锁定。
- 测试 handler 不依赖实时刷新，创建后 reset aggregator 不改变 share 内容。

### Iterator 4：公开读取能力对齐

- 与宿主确认并实现 public resource/dynamic route contract；若宿主不提供，则先实现固定 `/share.html` 资源和明确的 blocking API 适配，不提交伪动态路由。
- 加入错误统一 404、token cookie、过期行为测试。

### Iterator 5：Dashboard Compare UI

- toolbar 入口、选择器、range/metric 控件、四卡片、归一化 uPlot、排名表。
- 保持单文件资源和现有响应式 CSS。
- 在无数据、鉴权失败、选择数量越界时验证 UX。

### Iterator 6：Share UI + CSV

- share-only 路由初始化、只读标识、Share modal、过期选项、复制 URL。
- 前端 Blob CSV 和 RFC 4180 测试/手工验证。
- 运行 Go tests 和浏览器 smoke test。

### Iterator 7（P2）：PNG

- 仅在 MVP 用户验证需要整页图片后引入 html2canvas；固定依赖并验证单文件体积、CSP、字体、长页面和 uPlot canvas。

## 10. 验收标准

- Compare 仅允许 2–6 个 subject，四项原始指标和排名方向正确。
- 主图默认 normalized，不出现双 Y 轴歧义；tooltip 能读原始值。
- 创建快照后实时数据变化、reset、重启均不改变快照内容。
- 默认分享无需登录；require token 分享在无/错误 token 时不可读，正确 token 可读并可复制到新浏览器。
- 过期和手动删除均返回不可区分的 404；清理不会删除未过期快照。
- CSV 在 Excel 和常见表格工具中正确打开，字段含逗号/引号/换行时不破坏列。
- PNG 明确标为 P2，不影响 MVP 交付。
- 不引入数据库、不修改现有 runtime persist 文件语义、不引入新的图表库。

## 11. 实施前阻塞决策

宿主的 `ManagementRoute` 是精确匹配且 Management API 默认已鉴权，`ResourceRoute` 虽公开但当前路由静态、不能安全地提供动态 JSON。正式开工前必须确认宿主是否愿意提供一个公开插件动态 route 能力，或接受一个固定公开 share-data route 的 query 方案；否则“公开 `/v0/management/stats/share/{short_id}`”无法仅由本插件实现。这是架构约束，不应通过前缀匹配或绕过 host 鉴权解决。
