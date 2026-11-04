# Proposal

## Why

主链判据「含会话首步者为主链」在真实流量下失效。Claude Code 会先发出一次「生成会话标题」请求。它是会话的第一步，但只出现一次。

实测会话 `1:141983eb-874a-4072-abbf-7bfd343e6efb`（截至提案时）：

| 链 | 步数 | 首步时间 |
|---|---|---|
| 生成标题 | 1 | 13:04:08.939 |
| 主对话 | 20 | 13:04:08.965 |
| 安全监控 | 12 | 13:04:12.050 |

标题链比主对话早 26 毫秒。当前实现把标题链定为主链。结果：主路统计只有 1 步，主对话与监控都被标成旁路。会话头显示「主链 1 条、旁路 2 条」，读者无法认出主对话。

旧判据的前提「会话第一步是用户对话」不成立。规范未覆盖这类一次性自动请求。

## What Changes

- 主链判据改为两级：
  1. 先排除不足两个非重复步的链。重试只重复已有内容，不计入门槛。
  2. 在剩余链中取最早出现的那条。
  3. 若没有至少两个非重复步的链，退回含会话首步者。
- 「非重复步」只作门槛，不作排序依据。主链不按步数或长度选择，规范记录的旧反例仍成立。
- 两份主规范同步修改：`trajectory-analysis` 的链归属、`admin-console` 的会话链汇总。
- 实现改动集中在 `trace.AnalyzeSession` 与 `pkg/admin` 的链编号。管理面沿用「主链 / 旁路链」字段，不新增接口字段。
- 控制台链汇总的工具提示同步改为主链新判据。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `trajectory-analysis`：链归属的要求改为「持续性优先」的主链判据，并补充一次性自动请求的场景。
- `admin-console`：会话链汇总的主链定义同步修改，注明主链仍恒为 1 条。

## Impact

- `pkg/trace/chain.go`：`AnalyzeSession` 选择主链的逻辑与文档注释。
- `pkg/admin/traces.go`：旁路链编号不再假设主链位于下标 0。
- `pkg/admin/trace_json.go`：链汇总注释、story 与 detail 的链首文案。
- `pkg/trace/chain_main_test.go`、`pkg/admin/main_chain_test.go`：新增回归用例，覆盖首步为一次性请求的会话、重试与多旁路编号。
- `web/src/trailLegend.ts`、`docs/trail-view.html`：链汇总工具提示同步新判据。
- 历史数据无需迁移。链归属在查询时计算，不改写已存数据。
