# Proposal

## Why

分类词「新链」与链归属的「链」指向两件事,读者反复误读:看到「新链 2」会以为开了 2 条链。真实含义是「这两步与**同链**上一步逐字节零重合」——它描述内容断层,不描述链的条数。

链归属另有判据(稳定指纹:剔除易变行后的 system 前缀加首条 user 前缀),与内容比对无关。两个判据的原料不同,所以「同一条链」与「零重合」可以同时成立。

## What Changes

- 分类 `chain` 的显示词从「新链」改为**「无重合」**。徽标、分布条、图例、判定理由四处同步。
- 判定理由改述事实:与同链上一步无共同前缀也无共享尾部。**不得** 再出现「新链」这一说法。
- 轮级标记「⇄ 新链」改为「⇄ 不相交」:它同样由该分类推导,含义是「本轮与上一步不相交」。
- 规范用词同步:`trajectory-analysis` 的分类名、`admin-console` 的徽标与图例措辞,以及 `trail-view-readability` 与 `kinds-bar-caliber` 提及该分类的条目,一并改为「无重合」。四个变更都未归档,此刻改词最省事。合并进主规范后就不会再出现两套说法。
- 内部键 `kind = "chain"` **不变**:不动数据、不动接口、不动历史快照。
- 形态基准 `docs/trail-view.html` 同步。

**BREAKING**: 无。只改显示词与文档用词。

## Capabilities

### New Capabilities

无。本变更统一既有分类的显示用词。

### Modified Capabilities

- `admin-console`: 徽标、分布条、图例与判定理由改称「无重合」,轮级标记改称「不相交」。

## Impact

- 代码:`pkg/trace/delta.go`(判定理由)、`web/src/views/TracePanel.vue`、`web/src/trailLegend.ts`、`docs/trail-view.html`、`scripts/trail-view-smoke.mjs`、`pkg/trace` 与 `pkg/admin` 里提及该分类的注释与测试期望。
- 规范:`trace-system`、`trail-view-readability`、`kinds-bar-caliber` 三个未归档变更的用词同步修订。
- 接口:无变化(分类键仍是 `chain`)。
- 依赖:无新增。
