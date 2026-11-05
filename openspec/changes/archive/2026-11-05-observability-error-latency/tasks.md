# Tasks

## 1. 查询层

- [x] 1.1 在 `pkg/config/queries/requests.sql` 中给请求聚合增加 `errors_4xx` 与 `errors_5xx` 条件求和列。运行 `make generate`，确认生成文件更新。
- [x] 1.2 新增错误原因分组查询与成功请求延迟查询。两者沿用现有时间、key 与 session 过滤。运行 `make generate`。
- [x] 1.3 更新 store 包装与调用点。为 4xx / 5xx、错误原因、成功延迟增加表驱动测试。覆盖无筛选、仅 key、仅 session、叠加与无成功请求。运行 `go test ./pkg/config/ -run 'Error|Success|Aggregate'`。

## 2. 指标接口

- [x] 2.1 扩展 `handleRequestMetrics`，输出 `errors_4xx`、`errors_5xx`、`error_kinds`、`success_count`、`success_p95_ms`、`success_p99_ms`。运行对应 handler 测试。
- [x] 2.2 增加 handler 测试。覆盖错误原因空值归入未归因、成功延迟排除失败请求、无成功请求、筛选叠加与样本不足。运行 `go test ./pkg/admin/ -run Metrics`。

## 3. 前端明细

- [x] 3.1 扩展前端 `RequestMetrics` 类型，加入新字段。运行 `pnpm typecheck`。
- [x] 3.2 在 KPI 卡下方增加错误构成与成功延迟两行。错误原因为空时显示「未归因」，超过 6 项显示前 6 项与「其他」。无错误显示「无错误」，无成功请求显示 `—`。
- [x] 3.3 成功样本少于 20 时显示对应分位数提示。确认不新增同尺寸 KPI 卡。运行 `pnpm typecheck`。
- [x] 3.4 构建前端并检查页面。确认明细在卡下方，筛选变化后同步刷新。运行 `pnpm build`。

## 4. 回归与交付

- [x] 4.1 补充回归测试：错误原因计数之和等于错误总数，成功请求数加错误数等于请求总数。运行 `make test`。
- [x] 4.2 运行 `make coverage`，确认 `pkg/admin` 与 `pkg/config` 覆盖率不低于 97%。
- [x] 4.3 运行 `make lint`、`make smoke` 与 4 个工件的文字检查，确认全部通过。
- [x] 4.4 手工验收：制造或找到 4xx、5xx、流中断与成功请求，检查错误构成、成功延迟与「未归因」。确认筛选后明细同步。记录验收结果。
