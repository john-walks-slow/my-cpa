# 检视报告 v4（fix-verification）

## 概要

本轮复核 v3 遗留项 B-14～B-19、S-09～S-15 及 v4 变更代码、测试和总结文档。B-16、B-17、B-18、B-19、S-11 已有对应实现；但 B-14 的“加权 reservoir”实现仍不能证明请求级 P95 正确，且 B-15 明确仍会在重启后静默丢失历史 TTFT P95。核心指标正确性仍存在阻塞问题，因此本轮不准入。

## 需求对齐

- **B-14：未充分解决。** `accumulator.add` 引入了按 `TTFTReservoirCount()` 分配样本名额及硬 cap，但当前分配依赖增量累积状态，并且“加权”测试分别比较两个 subject，未构造同一 subject 内重轻 bucket 的合并场景，无法验证 v3 所指出的核心缺陷已修复。
- **B-15：未解决，且被文档接受为限制。** `persist.Restore` 不恢复 TTFT reservoir；恢复后的报告将 `p95_ttft_ms` 计算为 0，前端显示为 `—`。这仍不满足 Compare/Share 对持久化历史统计正确性的要求。
- **B-16：已解决。** tooltip 使用 DOM 节点、`textContent` 和 `replaceChildren`，未发现 label 进入 HTML 解析的路径。
- **B-17：已解决。** `uPlot.iife.min.js` 位于 `app.js` 之前加载；静态依赖顺序正确。
- **B-18：基本解决。** tooltip 会列出当前时间点全部 subject，且使用 `clientX/clientY` 配合 chart body rect 定位。仍缺真实浏览器 smoke 证据。
- **B-19：已解决。** CSV 的 `point.at` 和 `point.value` 使用 `??`，合法零值不会被转为空字符串。
- **S-11：已解决。** share-only 显示快照创建/过期信息，并将 compare count 改为只读说明。
- **S-09、S-10、S-12、S-13、S-14、S-15：按文档仍未完全处理。** 其中 S-09 是产品语义决策；其余为建议或已知限制。

## 阻塞问题

| ID | 位置 | 问题 | 建议 |
| --- | --- | --- | --- |
| B-14-v4 | `plugin/compare/compare.go:241-279` | 当前实现按 `a.ttftObserved` 的增量值计算每个 bucket 的 share，最终 cap 又按当前追加顺序进行步长抽样，因此结果依赖 bucket 遍历顺序；早期低权重 bucket 可能贡献过多样本，不能保证最终样本集合按总体请求分布采样。 | 先收集所有 contributing bucket 的 reservoir、observed count 和总 observed，再一次性按权重分配名额；或者采用真正的加权二次 reservoir sampling/可合并 quantile sketch。增加同一 subject 重轻 bucket、正序/逆序测试。 |
| B-15-v4 | `plugin/persist/persist.go`；`plugin/compare/compare.go:245-295` | v4 将重启后的 `ttftObserved == 0` → P95 为 0/前端 `—` 记录为 known limitation，但这仍使恢复后的 Compare/Share 报告缺少历史 TTFT P95。API JSON 中仍为数值 0，无法区分真实 P95 为 0 与历史样本不可恢复。 | 将 TTFT quantile 状态纳入持久化并测试 save→restore→Compare；若产品决定不持久化，则输出显式 `null`/可计算性标志并在 UI 标记不可用，不能静默用 0 表示缺失。 |
| B-20 | `plugin/compare/compare_test.go:180-223` | `TestTTP95WeightedBucket` 中 heavy 和 light 是不同 subject，各自内部 bucket 样本量相同，没有测试同一 subject 内重轻 bucket 合并；`TestTTP95Cap16K` 只验证长度，不验证分布或顺序无关性。 | 重写测试为同一 subject 至少两个 bucket：一个代表 100× 请求且低 TTFT，一个代表少量请求且高 TTFT；覆盖 reservoir 截断、16K+1 和反向遍历顺序。 |

## 建议修改

| ID | 位置 | 问题 | 建议 |
| --- | ---- | ---- | ---- |
| S-09 | `plugin/compare/compare.go:209-214`；`plugin/dashboard/web/dist/index.html:49` | v4 保留“前置 24h vs 当前报告范围”语义并仅通过文案说明；表头仍为“24h change”，不同 preset 比较不同长度窗口。 | 产品决策后统一等长相邻窗口，或改字段/表头明确“当前范围 vs 前置 24h”，并增加各 preset 测试。 |
| S-10 | `plugin/share/store.go`；`plugin/share/store_test.go` | 未实施真正的 enforce 删除失败注入，rollback 删除错误仍可能留下新快照。 | 抽象可注入文件操作或 remover，稳定模拟淘汰失败及 rollback 失败。 |
| S-12 | `plugin/dashboard/web/dist/app.js:29-30` | CSV 未防护 `=`, `+`, `-`, `@` 公式注入。 | 按目标办公软件策略处理危险前缀，或明确受信输入边界并补验证。 |
| S-13 | `plugin/management_handler.go` | TLS 头逗号链场景仍未处理，安全性依赖代理清洗。 | 对齐宿主代理契约，补逗号链和伪造头测试及部署说明。 |
| S-14 | `plugin/dashboard/web/dist/index.html:112` | 空 `#share-modal` 仍保留。 | 确认无外部契约后删除或说明兼容用途。 |
| S-15 | `plugin/compare/compare.go:39-41` | `Point.Raw` 固定命名 `raw_ttft_ms`，但所有 metric 都把当前 metric 值写入 `Value`，前端不使用 `Raw`。 | 统一 raw 字段、删除冗余字段或标记 schema 版本策略。 |
| S-16 | `plugin/dashboard/web/dist/app.js:27-28` | v4 仍使用 `innerHTML` 生成 KPI 静态结构，扩大未来注入回归面。 | 使用 DOM API/textContent，或限制模板为完全静态字符串。 |

## 非阻塞问题

| ID | 位置 | 问题 | 建议 |
| --- | ---- | ---- | ---- |
| N-01 | `plugin/dashboard/web/dist/app.js:28` | 缺失 subject 点统一显示 `—`，没有明确说明该时间点无数据。 | 在 tooltip 中区分无数据和真实空值。 |
| N-02 | `plugin/dashboard/web/dist/index.html:112` | 空 `share-modal` 占位增加维护噪声。 | 与 S-14 一并清理。 |
| N-03 | `plugin/compare/compare_test.go:225-239` | cap 测试没有断言 hard cap 后 P95 不因追加顺序显著变化。 | 增加顺序反转和分布断言。 |
| N-04 | `docs/features/260722-compare-share/260722-compare-share.validation.md` | 仍无宿主级浏览器 smoke 证据；node 语法检查不能验证运行时依赖、tooltip 定位、分享只读边界或 CSV 下载。 | 交付前填写真实公开 URL、token、重启、CSV 和浏览器交互结果。 |

## 准入结论

**结论**：`不准入`

**说明**：B-16、B-17、B-18、B-19、S-11 已基本落地，但 B-14 的实现与测试尚未证明总体请求加权 P95 正确，B-15 仍明确导致重启后历史 P95 缺失；两项均影响 Compare/Share 核心数据可信度。修复或正式调整 B-15 产品契约，并补足同一 subject 跨 bucket 权重测试及宿主级 smoke 后，需重新检视。

# 检视报告 v5（fix-verification）

## 概要

v5 在 v4 检视基础之上重做了 weighted P95 算法并加入缺失标记。`plugin/compare/compare.go` 重写为 deferred-merge（contribution 列表 + 全局权重一次性分摊），`ttft_observed` 字段已下发并贯穿 API→前端→CSV；KPI 卡不再使用 `innerHTML` 模板。B-14 的顺序无关性在算法和测试两端得到落实；B-15 的 API/前端/CSV 三处一致，只差一处 CSV 列在 `ttft_observed === 0` 时仍保留 0 而非空字符串。准入条件性通过。

## 需求对齐

- **B-14 加固**：已落实。`accumulator.add`（`plugin/compare/compare.go:288-305`）只保留 `ttftContribution{samples, weight}`，`weightedSample`（`plugin/compare/compare.go:375-421`）以 `totalWeight`（全局）按 `int(16384 * c.weight / totalWeight)` 计算 share，并在溢出后做一次 `shares[i] * 16384 / allocated` 二次缩放、保留 weight>0 时下限 1、合并前硬截断到 `maxTTP95Samples`。share 计算与 bucket 遍历顺序解耦，add 路径不引入任何顺序敏感的中间状态。
- **B-15 区分缺失**：API/前端两处已落实，`ttft_observed` 暴露。CSV 路径仍有一处与本轮声明的契约不一致：`p95_ttft_ms` 列在 `ttft_observed == 0` 时写入 `0` 而非空字符串（详见 `plugin/dashboard/web/dist/app.js:33`），与 S-25 同源。
- **S-16（KPI `innerHTML`）**：KPI 卡循环（`plugin/dashboard/web/dist/app.js:30`）改用 `createElement` + `textContent` 组装，模板仅剩静态 `cards.innerHTML = ""` 用于清空，已大幅缩小注入回归面。
- **S-09（24h-over-24h）**：`trendBoundaries`（`plugin/compare/compare.go:177-182`）与 `computeTrend24hOver24h`（`plugin/compare/compare.go:244-270`）将 trend 锁定为最近 24h vs 前 24h，与报告主范围解耦；JSON 字段名 `trend_24h_over_24h` 与新增测试 `TestTrend24hOver24h` 一致。

## 阻塞问题

无。

## 建议修改

| ID  | 位置 | 问题 | 建议 |
| --- | ---- | ---- | ---- |
| S-25 | `plugin/dashboard/web/dist/app.js:33`（CSV 行 `p95_ttft_ms` 列） | 当 `row.ttft_observed === 0` 时，本轮明确声明的契约要求 `p95_ttft_ms` 单元格写空字符串，但代码直接写入 `row.p95_ttft_ms`（值为 0），与 v5 任务说明以及 B-15 复核要求"不能静默用 0 表示缺失"仍有出入。 | 增加 `p95Cell = (row) => (row.ttft_observed \|\| 0) > 0 ? row.p95_ttft_ms : ""` 类似的辅助并替换该单元；写出的 CSV 可被 Excel/Numbers 解析为"空"，消费者依靠 `p95_ttft_observed` 列判定"无观测"。 |
| S-26 | `plugin/compare/compare.go:49`、`plugin/dashboard/web/dist/app.js:33` | JSON 字段命名是 `ttft_observed`（位于 `Row`），但 CSV 列名是 `p95_ttft_observed`，命名不对称给消费端带来不一致心智模型。 | 二选一：把 CSV 列改名为 `ttft_observed` 与 JSON 对齐，或在 schema 文档里明确两套命名的等价关系（建议前者）。 |
| S-27 | `plugin/dashboard/web/dist/app.js:30`（KPI 卡） | KPI 卡只为 `p95_ttft_ms` 字段做了 `ttft_observed === 0 → "—"` 处理；其余三个 KPI（`avg_stream_rate_tps`、`success_rate`、`avg_latency_ms`）直接 `format(subject[field])`，当 `subject.count === 0` 或 `subject.streamCount === 0` 时仍会输出 `0.0 tps`、`0.0%`、`0ms` 等看似真实的零值。top-ranked subject 在正常 Compare 流程中通常有数据，corner case 处理已足，但存在"P95 top 但其他指标空"场景时显示 `0ms / 0%` 与 `—` 不一致。 | 把四个 KPI 字段统一接 `metricAvailable`/`hasTTFTSamples` 判断（前者按当前 metric，后三者按 `count > 0`），让"无数据"的视觉信号一致为 `—`。属于体验一致性，非必须。 |
| S-28 | `plugin/compare/compare.go:288-305`（`accumulator.add`） | 单次 Compare 请求把每个 contributing bucket 的整个 TTFT reservoir（最长 1024）一并缓存在 `ttftContribs` 中。对于 7d + 1h window 范围（168 buckets）峰值占用约 168×1024×8B ≈ 1.4MB 单 subject；多 subject compare 并发时内存放大但仍有限。 | 现状可接受。如果未来 Compare/Share 范围扩大或引入更多 metric 的同等 reservoir，建议显式在 `accumulator` 上加 `preallocated cap` 或在 `add` 内按 weight 比例裁剪 sample 数。要落地的代价是改动较大，建议列入下个迭代 backlog 而非本次范围。 |

## 非阻塞问题

| ID  | 位置 | 问题 | 建议 |
| --- | ---- | ---- | ---- |
| N-08 | `plugin/compare/compare.go:177-182`（`trendBoundaries`） | 返回 4 元组但调用方仅用了前 3 个值（`currentFrom, currentTo, previousFrom, _`），第 4 个值 `previousTo` 在函数内赋值为 `currentFrom`（符合"两个 24h 窗口背靠背"的语义），调用方丢弃，建议命名/文档更明确。 | 给函数加注释或拆为 `trendCurrentRange`/`trendPreviousRange` 两个返回 2 元组的辅助函数，自文档性更佳。 |
| N-09 | `plugin/dashboard/web/dist/app.js:27`（`metricAvailable`） | `(row.ttft_observed \|\| 0)` 多此一举：`ttft_observed` 是后端声明的 `uint64`，到 JS 永远是非负整数，没有 falsy 风险；后续两分支 `(row.count \|\| 0) > 0` 同上。 | 简化为 `row.ttft_observed > 0` 与 `row.count > 0`，与后端类型契约一致；保持可读但冗余度更低。 |
| N-10 | `plugin/compare/compare_test.go:230-254`（`TestTTP95OrderIndependent`） | 测试构造的两条 contribution 的 `samples` 都是常量值（`10ms`/`500ms` 各 64 个），因此 `weightedSample` 对它的输出是**确定性**的（heavy 桶 sample with replacement 也恒等于 `10ms`），但断言仍写成 `p95f > 20*time.Millisecond` 容差，没有断言"正序 = 逆序"严格相等。 | 在重随机场景下严格相等不可得；当 `samples` 全是常量时可断言 `p95f == p95r == 10ms` 以加强信号，或加注释解释为何用 `< 20ms` 兜底。 |
| N-11 | `plugin/compare/compare_test.go:269-286`（`TestTTP95Cap16K`） | 该测试只断言 `len(merged) <= maxTTP95Samples`，没有断言 `merged` 内分布期望（heavy 主导或最后长度严格等于 `maxTTP95Samples`）。与新加的 `TestTTP95StrictCap16KPlusOne` 形成补充但本测试承担重叠职责。 | 可考虑把"自然下采样 step=1 的密集场景"与"16K+1 单样本严格 cap"合并为参数化用例，或删除 `TestTTP95Cap16K` 中的 `1000 × 满 reservoir` 场景以减少冗余。 |
| N-12 | `docs/features/260722-compare-share/260722-compare-share.validation.md` | 自动化测试结果清单中关于 v5 的 `TestTTP95OrderIndependent` / `TestTTP95SameSubjectWeightDiff` / `TestTTP95ReservoirTruncation` / `TestTTP95StrictCap16KPlusOne` 尚未补录；v4 文档也未引用 v5 的 CSV `p95_ttft_observed` 列更新。 | 在 `validation.md` 中追加 v5 测试说明；在 `summary.md` 中确认 CSV header 与列对齐；保证文档/测试/代码三处一致。 |
| N-13 | `docs/features/260722-compare-share/260722-compare-share.review.md` 本节 | 上一轮 v4 留下 N-04 / B-20 / S-09 等多项也未明确关闭或跟随状态；本轮应给出每项的 follow-up 状态。 | 在 v4 表格里加一栏"v5 状态"或在本节顶部列出"已关闭 / 仍在 backlog"的迁移说明，方便下轮检视快速定位增量。 |

## 准入结论

**结论**：`条件准入`

**说明**：v5 把 B-14 顺序无关性从算法到测试完整落地，`weightedSample` 以全局权重一次性分摊且测试 `TestTTP95OrderIndependent` 验证 forward/reverse 同向收敛；B-15 通过 `Row.TTFTObserved`（JSON `ttft_observed`）配合前端 `metricAvailable` / `formatMetricCell` / `trendAvailable` 与 KPI 卡 `createElement` + `textContent` 路径已正确区分缺失与零值。阻塞问题清零。仅存一处建议级契约偏差：CSV `p95_ttft_ms` 列在 `ttft_observed === 0` 时仍写入 `0` 而非空字符串，与本轮明确声明的契约不符，请按 S-25 在合并前修掉；若团队接受"消费端以 `p95_ttft_observed` 判断缺失"的折衷，可放宽至合并后迭代处理。其余建议项与 v4 backlog 一并作为后续清理项，不应阻塞交付。
