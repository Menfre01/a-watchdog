# Design

## Context

请求指标当前返回总数、错误数、p50 / p95 / p99、缓存率与 token。失败请求与成功请求混在同一组延迟样本里。`requests` 表已经有 `status_code`、`error_kind` 与 `latency_ms`，不需要新增字段。

本变更依赖 `observability-metrics-filter` 先归档。两个变更都覆盖「请求指标岛」这一条完整需求，后归档的 delta 会覆盖前一个。因此 apply 与 archive 顺序必须是筛选变更在前。

## Goals / Non-Goals

**Goals:**

- 给出 4xx、5xx 与错误原因计数。
- 给出成功请求数、成功请求 p95 与 p99。
- 保持现有指标字段与筛选行为不变。
- 用紧凑明细显示，不增加同尺寸 KPI 卡。

**Non-Goals:**

- 不统计首字节延迟。
- 不计算成本。
- 不按 provider 或 model 拆分。
- 不做时间序列或趋势图。
- 不改变错误判定口径。

## Decisions

### 1. 错误构成拆成状态码与原因两组

`AggregateRequestsSince` 增加两个条件求和列：

- `errors_4xx`：`status_code BETWEEN 400 AND 499`。
- `errors_5xx`：`status_code >= 500`。

错误原因单独查询：

```sql
SELECT COALESCE(error_kind, '') AS kind, COUNT(*) AS count
FROM requests r
WHERE ...同一组筛选...
  AND (r.status_code >= 400 OR r.error_kind <> '')
GROUP BY COALESCE(error_kind, '')
ORDER BY count DESC, kind
```

4xx / 5xx 与错误原因不是同一维度。状态码 200 但 `error_kind` 非空的流中断只出现在原因组。界面必须把两组分开显示，不能相加成一个饼图。

空原因由界面显示为「未归因」。原因超过 6 项时，界面显示前 6 项与「其他」，但错误总数仍来自 `errors`。

### 2. 成功请求延迟单独查询

成功请求指不属于错误的请求。SQL 条件：

```sql
status_code < 400 AND (error_kind IS NULL OR error_kind = '')
```

新增 `ListSuccessfulRequestLatenciesSince`，使用与现有延迟查询相同的筛选与排序。成功请求数取返回列表长度。p95 / p99 复用现有 `percentile`。

不把状态码带进现有 `ListRequestLatenciesSince` 再在 Go 侧过滤。这样会把失败请求也读出来，样本大时浪费。

### 3. 指标接口增加字段

`GET /console/api/metrics` 的响应增加：

- `errors_4xx`、`errors_5xx`：整数。
- `error_kinds`：数组，元素为 `{kind, count}`，按 count 降序、kind 升序。
- `success_count`：整数。
- `success_p95_ms`、`success_p99_ms`：整数。

现有字段保持原义。没有成功请求时，成功延迟返回 0。界面显示 `—`，不显示 `0ms`。

### 4. 指标岛用一条紧凑明细承载

KPI 卡保持六张。卡下增加两行：

```
错误构成   4xx 2 · 5xx 1 · 上游 2 · 未归因 1
成功延迟   p95 2.3s · p99 3.1s（样本 18）
```

原因用普通文字与分隔点，不使用新的卡片或大色块。错误为 0 时显示「无错误」。成功样本为 0 时显示 `—`。成功样本少于 20 时追加一句分位数提示。

### 5. 所有新指标沿用现有筛选

错误构成与成功延迟必须使用当前时间、key 与 session 前缀条件。session 筛选继续只覆盖有快照请求。查询层复用同一组过滤谓词，测试覆盖无筛选、仅 key、仅 session、叠加。

## Risks / Trade-offs

- **错误原因基数不可控** → `error_kind` 来自网关归因，当前取值有限。界面仍限制前 6 项，避免撑开布局。
- **读者可能把 4xx / 5xx 与原因组读成可相加** → 两组分开标注。流中断等状态码 2xx 的错误只出现在原因组。
- **成功样本少** → p95 / p99 不稳定。显示样本数并在少于 20 时提示。
- **SQL 谓词重复** → 现有聚合、全部延迟与成功延迟三处过滤条件需要保持一致。用共享测试表驱动覆盖。
- **归档顺序** → 先归档 `observability-metrics-filter`，再归档本变更。否则完整需求块会丢失筛选条款。

## Migration Plan

- 更新 sqlc 查询并重新生成代码。
- 无数据库迁移。
- 接口字段只增不改。旧前端忽略新字段。
- 先发布后端，再发布前端明细。
- 回滚时前端隐藏明细，后端保留新增字段即可。
