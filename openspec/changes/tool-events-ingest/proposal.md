# Proposal

## Why

工具执行在客户端完成。网关只看得到两次请求之间的间隔。间隔里混着工具执行、客户端处理与人的思考，凭它推断耗时并不准确。

客户端在工具执行前后各上报一次，就能按 `tool_use_id` 精确配对。误差压到 hook 分发与网络这一级。

## What Changes

- 新增上报端点 `POST /v1/tool-events?client=<claude-code|codex|dsh>`，复用客户端密钥鉴权。
- 新增 `tool_events` 表与 0004 迁移。按 `(client, tool_call_id, phase)` 幂等入库。
- 服务端按 `client` 适配载荷。Claude Code 与 Codex 取 `hook_event_name` 与 `tool_use_id`，DSH 取规范字段。
- 事件记录边界类型与审批等待标记。时长口径写明是事件到达网关的墙钟差。
- 只有开始事件的调用不推算时长，标记为未完成。

## Capabilities

### New Capabilities

- `tool-timing`: 工具事件上报的接口、鉴权、幂等与时长口径。

### Modified Capabilities

无。

## Impact

- 数据库：新增 `tool_events` 表与迁移。
- 后端：新端点、适配层、存储方法与测试。
- 客户端：Claude Code 用 HTTP hook，Codex 用 command hook，DSH 用插件。
- 控制台：本轮不改界面，数据先落库。
