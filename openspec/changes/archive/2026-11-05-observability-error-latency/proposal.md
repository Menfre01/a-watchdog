# Proposal

## Why

请求指标目前只给总错误率与全部请求的 p50 / p95，无法看出错误由状态码还是上游原因造成。失败请求通常很快返回，混在一起会把延迟压低。管理员需要一组可行动的错误构成与成功请求延迟。

## What Changes

- 指标接口增加错误构成：4xx 数、5xx 数、按 `error_kind` 分组的原因计数。空原因归入「未归因」。
- 指标接口增加成功请求延迟：成功请求数、p95、p99。成功请求指不属于错误的请求。
- 错误判定沿用现有口径：状态码 4xx 以上，或 `error_kind` 非空。
- 指标岛在 KPI 卡下方增加一条紧凑明细，不新增同尺寸卡片。
- 4xx / 5xx 与错误原因并列展示。错误原因过多时只展示前 6 项与「其他」。
- 成功请求样本少于 20 个时，界面说明分位数参考意义有限。
- 新指标沿用现有时间、key 与 session 筛选。session 筛选仍只覆盖有快照请求。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `admin-console`: 「请求指标岛」增加错误构成与成功请求延迟。

## Impact

- 后端：`pkg/admin/metrics.go`，`pkg/config` 的请求聚合与延迟查询，sqlc 生成文件。
- 前端：`web/src/views/MetricsIsland.vue`。
- 测试：指标 handler、store 查询、前端 typecheck 与构建。
- 无数据库迁移。无新增依赖。现有指标字段保持不变。
- 归档顺序：先归档 `observability-metrics-filter`，再归档本变更。
