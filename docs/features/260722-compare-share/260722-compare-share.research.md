# 260722 Compare/Share 调研结论

## 1. 调研范围

本次调研覆盖现有插件代码、宿主管理路由接口，以及 Grafana、Observable Plot、html2canvas、Nano ID 和浏览器 Blob 下载资料。关键事实如下：

- 当前插件的 Management API 在宿主层统一作为“已鉴权 Management API”分发；`ResourceRoute` 则明确是未做 management-authentication 的浏览器资源。
- 当前宿主只支持精确路由，不支持 `/stats/share/{short_id}` 这种动态路径参数。插件自行注册一个前缀路由也会被宿主的 route normalizer 拒绝。
- `aggregator.Snapshot()` 已经提供并发安全的深拷贝；现有 `persist.Store` 是单文件、定时覆盖写入的运行态恢复快照，不适合直接承载不可变分享数据。
- 当前聚合只保留固定窗口：1m、5m、15m、1h、24h；7d 和自定义时间窗不能只靠现有一个 bucket 查询实现，必须在报告生成时用可用桶重建时间序列，且超出 `retention_minutes` 的数据不存在。

## 2. 叠加曲线：选自动归一化，不选双 Y 轴

### Grafana

Grafana 的 Time series / XY chart 支持通过 field override 给不同序列分配左、右 Y 轴，并可按单位自动放置。它适合“温度 + 湿度”这类单位不同、仍希望读出绝对值的场景，但 Grafana 本身没有针对不同量纲自动归一化的通用功能；用户需要手工配置轴或范围。

### Observable Plot / Datalore 类实践

Observable Plot 明确不把双 Y 轴作为推荐能力，官方提供 `normalizeY`，用于把每条序列相对于 first、mean、min/max 或 extent 转成相对值。其理由是双轴容易让读者把视觉斜率和跨序列数值直接比较，且不同轴的自动范围会产生误导。Datalore 等 notebook/dashboard 产品通常也通过独立图、小 multiples 或显式 normalized/index 视图处理异量纲序列，而不是默认叠加双轴。

### 本功能结论

主图默认采用“按序列的 min-max 归一化到 [0, 1]”，图例和标题明确写明 `Normalized (relative shape)`，悬浮提示同时显示原始值。不要在 MVP 中做双 Y 轴：

1. 对比目标是“哪个 model/auth 的表现趋势更好”，而不是把 TTFT(ms)、TPS、成功率放在同一绝对坐标上。
2. 归一化不需要引入 uPlot 多轴插件，继续使用现有单文件 uPlot，减少静态资源和交互复杂度。
3. 排名卡片和表格保留原始单位，避免归一化隐藏真实业务含义。
4. 对常数序列（max=min）显示 0.5，避免 NaN，并在图例中保持可解释性。

归一化只用于主图，不用于排名。指标排序使用原始值和指标方向：TTFT/总响应时间越低越好，TPS/成功率越高越好。双 Y 轴可以作为 P2 的显式“绝对值模式”，要求用户选择同单位或明确轴标签，不能作为默认模式。

## 3. 快照存储

分享快照使用配置的数据目录下的 `shares/` 子目录，每个快照一个 JSON 文件；不引入数据库，也不复用运行态 `persist_path` 文件。原因是运行态 snapshot 会被周期性覆盖，而分享报告必须生成时锁定、可独立过期和删除。

建议：

- `persist_path` 非空时，shares 根目录为 `filepath.Dir(persist_path)/shares`。
- `persist_path` 为空时，使用配置的 `share_path`；若也为空则禁用 Share 并在 UI 提示需要配置持久化目录。
- 文件名为 `<short_id>.json`，ID 不来自自增序列。
- 使用临时文件写入后 rename，避免生成半文件；读取只解析单个快照。
- 定时清理过期文件，启动时清理一次，之后每小时清理一次；清理按 `expires_at`，永不过期文件跳过。
- 可设置 `share_max_count`，超限时按 `created_at` 删除最旧快照，防止永不过期分享无限占用磁盘。

PNG 调研结论见第 5 节。

## 4. 短 ID 与 token

短 ID 使用 `crypto/rand` 生成 10 字符 Base58（去掉易混淆字符 `0OIl`），通过 rejection sampling 保证字母表分布均匀。10 字符空间为 \(58^{10}\)，对本插件单实例、低创建量足够；写入前仍必须检查文件是否存在，冲突则重试。不要暴露自增 ID，也不要用时间戳作为唯一 ID。

“require token”不能只依赖 cookie：分享 URL 需要可以被复制并在新浏览器中打开。建议生成高熵随机 token，仅保存 SHA-256(token)；返回 URL 时只返回一次明文 token。访问时从 query token 或 `cpa_share_token` HttpOnly cookie 读取，哈希后 constant-time 比较。首次 query 校验成功时可设置 Secure（按请求）、HttpOnly、SameSite=Lax cookie，并重定向到不带 token 的短 URL，避免 token 长期留在 Referer。

HMAC 可作为可选增强：用 `share_secret` 对 `short_id || expires_at || snapshot_digest` 签名，防止 token 被跨快照复用。但 HMAC 不是必须的访问控制；MVP 采用每快照随机 token + hash，避免配置 secret 轮换导致全部旧链接失效。默认公开分享不保存 token hash，不要求登录。

## 5. PNG 截图

html2canvas 是纯浏览器方案，不需要服务端渲染，但它是重新实现 DOM/CSS 到 canvas，并非浏览器真实截图。资料显示它对部分 CSS、伪元素、复杂 SVG、外部字体和跨域资源存在限制；uPlot 本身使用 canvas，通常可被捕获，但整页高度、字体加载和响应式布局仍需专门处理。其单文件部署影响是需要额外 vendored 的压缩 JS（并扩大内嵌资源），且每次导出会增加内存和等待时间。

结论：PNG 整页截图列为 P2，不阻塞 Compare/Share MVP。MVP 先提供浏览器打印/“保存为 PDF”友好样式和 CSV；P2 再 vendoring 固定版本 html2canvas，导出前等待 `document.fonts.ready`，临时展开可滚动内容，限制最大像素面积，并明确截图是“当前渲染视图”而非服务端像素级截图。不要为了 PNG 引入浏览器自动化或服务端截图。

## 6. CSV

CSV 使用前端 Blob 生成。报告已经拿到原始快照数据，前端按 RFC 4180 规则转义逗号、双引号和换行，使用 UTF-8 BOM 改善 Excel 中文识别，创建 `URL.createObjectURL(blob)` 触发下载，完成后 `URL.revokeObjectURL(url)`。这比服务端新增导出 endpoint 更简单，也确保导出内容与当前只读报告完全一致。