# Design

## Context

`65fbf57` 把轨迹统计卡改为「机器时间」：模型与工具两段合计。同一提交删除了分类分布条与集中图例，但没有同步主规范、`docs/trail-view.html` 与冒烟脚本。三处继续要求旧元素，冒烟失败。修复冒烟时恢复旧界面，造成统计卡同时出现新旧两套元素。

## Goals / Non-Goals

**Goals:**

- 轨迹统计卡只保留模型 / 工具两段与链汇总，不再出现分类分布条与集中图例。
- 原型、冒烟脚本与规范与新形态一致。
- 删除只服务旧界面的导出，避免死代码再次被误用。

**Non-Goals:**

- 不改模型 / 工具耗时的计算口径。
- 不改分类键 `chain`、徽标文案与判定理由。
- 不为分类分布另建新视图。

## Decisions

### 决策 1：移除分类分布条与集中图例

`TracePanel.vue` 删除 `.kinds` 与 `.legend` 模板、相关导入与辅助函数。分类仍由每步徽标与判定理由呈现，`chain` 键与「无重合」文案不变。

### 决策 2：删除只服务旧界面的导出

`trailLegend.ts` 删除 `LegendEntry`、四个图例数组、`KIND_TEXT`、`KINDS_NEW_NOTE`、`NO_OVERLAP_NOTE`、`JUDGE_NOTE`。口径仍由 `AXIS_NOTE`、`HARNESS_NOTE`、`CHAIN_NOTE`、`START_NOTE`、`TURN_CHAIN_NOTE`、`USAGE_NOTE`、`MIXED_PROTOCOL_NOTE` 提供。

### 决策 3：原型保留轨迹主体，换成模型 / 工具统计

`docs/trail-view.html` 删除 `kindsBar()` 与 `legendHTML()` 的渲染调用，在会话头补一组「机器时间 / 模型 / 工具 / 工具占比」示例。原型仍遵守「实线实测、虚线推断」的视觉语言。

### 决策 4：冒烟改为校验新形态

冒烟删除分布条与图例的断言，增加「统计卡给出模型与工具」断言。构建产物检查只要求仍在使用的口径文案，不再要求图例数组。

## Risks / Trade-offs

- **分类分布不再一眼可见** → 读者需要展开 step 看徽标。若后续需要会话级分布，另立变更恢复为独立视图，而不是塞回统计卡。
- **原型与实现再次漂移** → 冒烟保留口径文案比对，并新增模型 / 工具断言。
- **旧导出被其他代码引用** → 删除前用 `grep` 确认无引用；`pnpm typecheck` 兜底。

## Migration Plan

无数据迁移。纯前端与文档改动，重启服务后生效。

## Open Questions

无。
