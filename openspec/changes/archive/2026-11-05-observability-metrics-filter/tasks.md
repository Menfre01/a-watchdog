# Tasks

## 1. 查询层

- [x] 1.1 在 `pkg/config/queries/requests.sql` 中给请求聚合与延迟查询增加可选的 `key_id` 与 `session_prefix` 参数。运行 `make generate`，确认生成文件更新。
- [x] 1.2 为请求聚合与延迟查询增加过滤测试。覆盖无筛选、仅 key、仅 session、key 与 session 叠加。断言两个查询使用同一范围。运行 `go test ./pkg/config/ -run 'Aggregate|Latencies'`。
- [x] 1.3 增加无快照请求测试。断言 key 筛选包含该请求，session 筛选排除该请求。运行 `go test ./pkg/config/ -run 'Snapshot|Filter'`。

## 2. 指标接口

- [x] 2.1 扩展 `handleRequestMetrics`，解析可选的 `key_id` 与 `q`。`key_id` 非法时返回 400。运行对应 handler 测试。
- [x] 2.2 更新 `AggregateRequestsSince` 与 `ListRequestLatenciesSince` 的 store 包装与调用点。确认未带参数时行为不变。运行 `go test ./pkg/config/ ./pkg/admin/`。
- [x] 2.3 增加 handler 测试。覆盖 key 筛选、session 筛选、两者叠加、无快照排除与非法 key。断言请求数与 p95 样本范围一致。运行 `go test ./pkg/admin/ -run Metrics`。

## 3. 前端筛选

- [x] 3.1 把筛选条移到指标岛之前，加入时间范围、key 与 session 前缀。会话详情打开时隐藏筛选条。确认列表随筛选自动刷新。
- [x] 3.2 在 `Observability` 中区分输入值与已应用值。时间默认 24 小时。时间与 key 变化立即应用，session 前缀防抖应用。
- [x] 3.3 让 `MetricsIsland` 接收已应用的时间、key 与 session，随 props 变化重新请求。删除指标岛自己的窗口按钮。运行 `pnpm typecheck`。
- [x] 3.4 在 session 筛选生效时显示「未生成快照的请求不计入」提示。确认无 session 筛选时不显示该提示。
- [x] 3.5 构建前端并检查页面。确认筛选条在指标岛上方，默认 24 小时，指标与会话列表使用同一组条件。运行 `pnpm build`。
- [x] 3.6 时间与 key 变化后立即应用筛选。session 前缀使用 300 毫秒防抖，按 Enter 立即应用。删除查询按钮，并在组件卸载时取消计时器。运行 `pnpm typecheck`。
- [x] 3.7 构建前端并检查交互。确认时间与 key 选中即刷新，session 输入后自动刷新，按 Enter 立即刷新。运行 `pnpm build`。

## 4. 回归与交付

- [x] 4.1 补充关键回归测试：同一时间范围与 key 下，列表计数与指标计数一致。运行 `make test`。
- [x] 4.2 运行 `make coverage`，确认 `pkg/admin` 与 `pkg/config` 覆盖率不低于 97%。
- [x] 4.3 运行 `make lint` 与 `make smoke`。对 4 个工件运行文字检查。确认全部通过。
- [x] 4.4 手工验收：选择时间与 key，检查立即刷新。输入 session 前缀，检查自动刷新与 Enter 立即刷新。确认筛选条没有查询按钮。切换「全部」后可见更早会话。记录截图或验收结果。
