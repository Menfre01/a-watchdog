# Proposal

## Why

轨迹统计卡把 steps 与 token 总量和「旁路」并排显示，但两者不是相加关系：旁路是总量的子集。读者分不清是「主路 + 旁路」还是「总 + 旁路」，容易把两行数字相加。

## What Changes

- 统计卡第一行标明 **总计**,给出 turns、steps、in/out/cache。
- 第二行拆成 **主路** 与 **旁路** 两组，各自给出 steps 与 in/out/cache。
- 「旁路」行明确标注为总计的子集，不得让读者以为需要相加。
- 接口的骨架响应新增 `main` 与 `side` 两组聚合。既有总计字段保留。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `admin-console`: 新增「统计口径拆分」要求，规定汇总必须区分总量与主路/旁路，并标明包含关系。

## Impact

- 后端:`pkg/admin/trace_json.go` 的骨架聚合。
- 前端:`TracePanel.vue` 的统计卡与 `api.ts` 的类型。
- 存储：无改动，聚合在读取时计算。
