# Proposal

## Why

轨迹统计卡已经换成「机器时间」：只给模型与工具两段合计。旧的分类分布条与集中图例在 `65fbf57` 已移除，但主规范、形态基准与冒烟脚本仍要求它们。冒烟因此失败，修复时又错误地恢复了旧界面，使统计卡同时出现新旧两套元素。

## What Changes

- 轨迹视图删除分类分布条与集中图例，保留「机器时间」的模型 / 工具两段。
- 删除只服务这两块的导出：`LINE_LEGEND`、`KIND_LEGEND`、`MARK_LEGEND`、`USAGE_LEGEND`、`KIND_TEXT`、`KINDS_NEW_NOTE`、`NO_OVERLAP_NOTE`、`JUDGE_NOTE`。
- `docs/trail-view.html` 同步删除分类分布条与图例，补上模型 / 工具统计。
- 冒烟脚本改为校验新形态：模型与工具两段存在，旧图例与分布条不再要求。
- `admin-console` 规范删除「分类分布条」相关表述，把用量口径场景改到协议标记。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `admin-console`：分类显示用词只约束徽标与判定理由；用量口径的说明位置从图例改为协议标记；轨迹统计卡以模型 / 工具两段为准。

## Impact

- 前端：`web/src/views/TracePanel.vue`、`web/src/trailLegend.ts`。
- 形态基准与测试：`docs/trail-view.html`、`scripts/trail-view-smoke.mjs`。
- 后端、接口、数据库：无改动。
