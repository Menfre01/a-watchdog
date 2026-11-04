# Tasks

## 1. 前端移除旧界面

- [x] 1.1 `web/src/views/TracePanel.vue` 删除分类分布条与集中图例的模板、导入、`kindPct`、`kindText`、`legendFrom` 与对应 CSS。
- [x] 1.2 `web/src/trailLegend.ts` 删除 `LegendEntry`、四个图例数组、`KIND_TEXT`、`KINDS_NEW_NOTE`、`NO_OVERLAP_NOTE`、`JUDGE_NOTE`。
- [x] 1.3 运行 `pnpm typecheck`，确认无未使用导入与类型错误。

## 2. 形态基准与冒烟

- [x] 2.1 `docs/trail-view.html` 删除 `kindsBar()` 与 `legendHTML()` 的渲染调用。
- [x] 2.2 原型会话头补模型 / 工具统计示例。
- [x] 2.3 `scripts/trail-view-smoke.mjs` 删除分布条与图例断言，新增模型 / 工具断言，构建产物检查不再要求图例数组。
- [x] 2.4 运行 `make smoke`，全部通过。

## 3. 规范

- [x] 3.1 新增 `admin-console` delta：分类显示用词只约束徽标；用量口径场景改到协议标记。
- [x] 3.2 运行 `openspec validate trace-summary-composition --strict`。

## 4. 构建与验证

- [x] 4.1 运行 `pnpm build`，确认产物不含图例数组。
- [x] 4.2 运行 `make test` 与 `make lint`。
- [x] 4.3 重建 `bin/awatchdog` 并重启 8399，确认统计卡只显示模型 / 工具两段。
