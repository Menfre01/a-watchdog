# Design

## Context

当前 `GET /console/api/metrics` 只接受 `window`。请求聚合与延迟查询都只按 `ts` 过滤 `requests` 表。会话列表走 `/console/api/traces`，已支持 `key_id`、`q`、`from` 与 `to`，但前端只暴露了前两个。

`requests` 表有 `client_key_id`，没有 `session_id`。session 归属写在 `snapshots` 行上。列表的 session 前缀匹配的是 `session_id` 冒号后的客户端原始标识。

## Goals / Non-Goals

**Goals:**

- 时间范围、key 与 session 前缀在筛选条中同级。
- 时间范围默认最近 24 小时，同时作用于指标与会话列表。
- 指标与会话列表共用 key 与 session 前缀条件。
- key 筛选覆盖该 key 的全部请求，包括无快照请求。
- session 筛选只覆盖有快照且前缀匹配的请求。
- 时间或 key 变化时立即刷新，session 前缀防抖后刷新，按 Enter 立即刷新。指标岛不再保留独立窗口按钮。

**Non-Goals:**

- 不做自定义起止日期。时间只保留 1 小时、24 小时、7 天、全部四档。
- 不把 session 筛选扩展到无快照请求。
- 不按 provider、model 或 Agent 分类聚合指标。
- 不改变指标计算公式与四档窗口定义。
- 不新增数据库迁移或索引。

## Decisions

### 1. 时间范围由页面统一计算，查询接口沿用现有参数

指标接口继续使用 `window`。列表接口继续使用 `from` 与 `to`。

时间或 key 变化、session 防抖到期或按 Enter 时，页面按所选档位计算一个 `from` 时间戳：

- 1 小时、24 小时、7 天分别减去对应时长。
- 全部不传 `from`。

列表请求使用这个 `from`。指标请求使用同一档位的 `window`。两个接口由不同请求计算边界，误差在毫秒级，不影响当前本地单用户场景。

列表按 `snapshots.ts` 过滤，指标按 `requests.ts` 过滤。快照写入时取请求的 `ts`，有快照的请求两列同源。没有快照的请求只出现在指标里。

备选方案：给指标接口增加 `from` 参数，让两个视图共用同一个时间戳。这样更精确，但会扩大接口并增加测试面。当前不采用。

### 2. 在 `requests` 查询上增加可选过滤，不预先物化

扩展现有 `AggregateRequestsSince` 与 `ListRequestLatenciesSince` 查询，增加两个可选参数：

- `key_id`：命中 `requests.client_key_id`。
- `session_prefix`：用 `EXISTS` 关联 `snapshots`，按冒号后的原始标识做前缀匹配。

用 `EXISTS` 而不是 `JOIN snapshots`，原因有两点：

- `snapshots.request_id` 唯一，不会重复计数。
- key 筛选仍会命中没有快照的请求。若改成内连接，key 筛选会静默丢请求。

两个查询必须使用同一组谓词。否则请求数与 p95 的样本范围会不一致。

备选方案：先按 session 查出请求 id，再由 Go 侧聚合。这样会多一次查询与内存拼接，SQL 过滤更直接，不采用。

### 3. session 前缀沿用列表语义

列表过滤使用：

```sql
substr(s.session_id, instr(s.session_id, ':') + 1) LIKE ? || '%'
```

指标查询复用同一表达式。原因：

- `snapshots.session_id` 带 `<key_id>:` 前缀，直接 `LIKE` 会匹配到错误的位置。
- 列表和指标必须对同一输入得到同一组会话。

### 4. 接口参数与错误处理

`GET /console/api/metrics` 增加：

- `key_id`：可选，正整数。空值表示全部密钥。非正整数返回 400。
- `q`：可选，session 标识前缀。空值表示不过滤。
- `window`：继续使用四档窗口。

`key_id` 不是正整数时返回 400。`q` 原样传给查询，与列表保持一致，不做额外 trim。

备选方案：用布尔参数区分“全部”和“未提供”。现有列表已用空值表达全部，指标沿用同一约定。

### 5. 前端把范围状态提升到可观测页

筛选条从列表容器内移到指标岛之前。`Observability` 保存输入值与已应用值：

- 输入值：`rangeFilter`、`keyFilter`、`query`。
- 已应用值：`appliedRange`、`appliedKey`、`appliedQuery`、`appliedFrom`。

时间与 key 的选择变化后立即更新已应用值。session 前缀输入后等待 300 毫秒再更新，按 Enter 立即更新。筛选条不设查询按钮。

更新已应用值时取消防抖计时器，避免旧输入在时间或 key 变化后又触发一次请求。组件卸载时也取消计时器。

列表用 `appliedKey`、`appliedQuery`、`appliedFrom` 请求。指标岛接收 `appliedRange`、`appliedKey`、`appliedQuery`，并在 props 变化时重新请求。默认 `appliedRange` 为 24 小时。页面首次加载时，列表也带最近 24 小时的 `from`。选择「全部」后，列表不带 `from`。

### 6. 指标岛去掉窗口状态

`MetricsIsland` 删除窗口按钮与本地窗口状态。窗口由 `Observability` 的筛选条控制。指标岛只在 props 变化时重新请求，保留自己的加载与错误状态。

### 7. 无快照提示放在指标岛内

当 `appliedQuery` 非空时，指标岛显示一句说明：按 session 筛选时，未生成快照的请求不计入。提示只在 session 筛选生效时出现。样本不足提示仍按原规则显示。

## Risks / Trade-offs

- **列表默认范围变化** → 列表从全部变为最近 24 小时。界面必须清楚显示当前范围，并提供「全部」选项。用验收场景覆盖。
- **输入触发多次请求** → session 前缀使用 300 毫秒防抖。时间或 key 变化立即刷新，同时取消待执行的防抖。
- **时间边界由两个请求分别计算** → 列表与指标可能相差毫秒。当前可接受。若后续要求严格一致，再给指标接口增加共享时间戳。
- **列表与指标的 key 过滤谓词不同** → 列表按 `session_id` 前缀，指标按 `client_key_id`。两者都由同一个 key 派生，正常情况下一致。用测试覆盖同一 key 下的计数，若出现不一致再统一。
- **两个 SQL 查询谓词漂移** → 请求数与 p95 范围不一致。为两个查询写同一组过滤测试，覆盖无筛选、仅 key、仅 session、全部叠加。
- **session 筛选排除无快照请求** → 指标小于全局值。这是预期行为，界面必须说明。
- **`EXISTS` 查询成本** → `snapshots.request_id` 唯一，按主键查找。当前数据量为本地单用户，成本可接受。

## Migration Plan

- 修改 sqlc 查询并重新生成代码。
- 无数据库迁移。
- 先发布后端可选参数。未带参数时行为不变。
- 再发布前端筛选条与默认 24 小时范围。
- 回滚时前端恢复旧位置与列表全量默认，后端保留可选参数即可。
