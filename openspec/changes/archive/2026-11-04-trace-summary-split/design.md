# Design

## Context

见 `proposal.md` 的 Why。`skeletonJSON` 的 steps 与 usage 是对全部步求和,`chainSummary` 另给旁路的 steps 与 in/out 子集，但没有旁路 cache。前端把总计与旁路并排渲染，未标包含关系。

## Goals / Non-Goals

**Goals:**

- 读一眼就知道总计包含旁路。
- 主路与旁路各有自己的 steps 与 token。

**Non-Goals:**

- 不改快照存储与聚合时机。
- 不改轮次、步、链的既有统计口径。

## Decisions

### 1. 聚合在读取时算

`skeletonJSON` 遍历步时按 `sideSteps` 分流，分别累加 steps、in/out/cache,输出 `main` 与 `side` 两个对象。总计字段保留，前端不必自己相减。

理由：旁路标记本来就在 `sessionAnalysis` 里，读取时多一次遍历没有额外成本。前端相减会漏掉 cache,因为旧响应没有旁路 cache。

### 2. 总计字段留在原位

`steps`、`usage_*` 不改名，保持既有消费方可用。新增 `main`、`side` 为附加字段。

### 3. 前端两行布局

第一行：总计(turns、steps、in/out/cache)。第二行：主路组与旁路组，旁路组标题带「总计的子集」说明。没有旁路步时整行不渲染。

## Risks / Trade-offs

- **旁路组缺 cache 的旧误解** → 新响应补齐旁路 cache。旧缓存响应不显示该字段时按 0 处理，并保持 `—` 语义。
- **数值重复风险** → 总计算一次、拆分算一次，两者含义不同。规范要求各自只出现一次。

## Migration Plan

无迁移。回滚为撤销新增字段与第二行。
