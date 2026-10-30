# Proposal

## Why

会话头的 `in / out / cache` 并排显示,但没有标出它们的口径。`in` 的含义随协议不同:Anthropic 的 `in` 不含缓存,而 OpenAI 兼容与 DeepSeek 的 `in` 含缓存命中。读者看一个后者的会话时,会以为 `in + cache` 是总输入量,实际重复计数。

## What Changes

- 每条步的骨架新增**协议**字段。协议已在记录侧(`requests.protocol`),本变更只把它交给视图。
- 会话头 **必须** 给出该会话出现的协议。出现多个协议时,视图 **必须** 说明各步按各自的协议口径记录用量,**不得** 直接相加。
- 图例新增一**用量口径**分组,写明两种口径:
  - Anthropic:`in` 是 `input_tokens`,不含缓存。`cache` 是缓存读加缓存写。`in + cache` 等于总输入。
  - OpenAI 兼容与 DeepSeek:`in` 是 `prompt_tokens`,已含缓存命中。`cache` 是它的子集。`in + cache` 会重复计数。
- 上述口径文案 **必须** 取自单一来源(前端常量表),原型与冒烟脚本共用同一批字符串。
- 形态基准 `docs/trail-view.html` 同步覆盖协议标记与用量口径分组。

本变更 **不** 改记录语义:入库的数字保持原样。归一化(把 `in` 统一改成未命中缓存的部分)不在本次范围。

**BREAKING**: 无。骨架接口只增字段。

## Capabilities

### New Capabilities

无。本变更只扩展现有控制台视图。

### Modified Capabilities

- `admin-console`: 轨迹视图新增协议标注与用量口径说明。

## Impact

- 代码:`pkg/admin/trace_json.go`、`web/src/views/TracePanel.vue`、`web/src/api.ts`、`web/src/trailLegend.ts`、`docs/trail-view.html`、`scripts/trail-view-smoke.mjs`。
- 接口:`GET /console/api/traces/{session_id}` 的每条 step 新增 `protocol`。只增不改。
- 依赖:无新增。协议字段已在库里。
- 数据:无迁移。历史数据的协议列已在记录侧填好。
