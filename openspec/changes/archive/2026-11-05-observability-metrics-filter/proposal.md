# Proposal

## Why

可观测页的请求指标一直是全局汇总，筛选条只作用于会话列表。时间范围又只存在于指标岛。管理员选中某个 key 或 session 后，列表与指标口径不一致，无法判断当前范围的请求质量。

## What Changes

- 筛选条移到请求指标岛之前。
- 筛选条把时间范围、client key 与 session 标识前缀放在同一级。三个条件可以叠加。
- 时间范围使用 1 小时、24 小时、7 天、全部四档，默认 24 小时。指标与会话列表共用这个范围。
- 时间与 key 变化后立即刷新。session 前缀输入后防抖刷新，按 Enter 立即刷新。筛选条不设查询按钮。
- 指标接口增加可选的 `key_id` 与 `q` 参数。请求数、错误率、p50、p95、缓存命中率与 token 都按同一范围计算。
- session 前缀通过 `snapshots.session_id` 关联请求。按 session 筛选时，没有快照的请求不计入指标。界面必须说明这一点。
- 指标岛不再提供独立的窗口按钮。
- 默认范围是最近 24 小时。key 与 session 为空。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `admin-console`: 「请求指标岛」增加时间范围、key 与 session 前缀筛选，并规定筛选条位于指标岛之前、两个视图共用条件。

## Impact

- 后端：`pkg/admin/metrics.go`，`pkg/config` 的请求聚合与延迟查询，sqlc 生成文件。
- 前端：`web/src/views/Observability.vue`，`web/src/views/MetricsIsland.vue`。
- 测试：指标 handler 与 store 查询测试，前端 typecheck 与构建。
- 无数据库迁移。无新增依赖。时间默认值从指标的 24 小时扩展为两个视图共用，会话列表默认从全部变为最近 24 小时。
