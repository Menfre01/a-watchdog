# Proposal

## Why

三类时间里，模型耗时是实测的，工具耗时靠钩子边界可得，**人的时间完全没有采集**。缺这一块，轨迹只能回答“机器花了多久”，回答不了“人想了多久”。

Claude Code 提供 `UserPromptSubmit` 事件。它的时间戳减去上一步结束时间，就是人从看到结果到再次提交的间隔。

## What Changes

- 新增 `prompt_events` 表与 0005 迁移，记录客户端每次提交提示的时刻。
- 新增上报端点 `POST /v1/prompt-events?client=…`，鉴权与工具事件一致。
- 新增读取接口 `GET /console/api/prompt-events?session_id=…`。
- 只存提示长度，不存提示正文。
- Claude Code 的转发脚本按事件名分流，`settings.json` 增加 `UserPromptSubmit` 钩子。

## Capabilities

### New Capabilities

- `prompt-timing`: 提示提交事件的采集与读取，用于计算人的间隔。

### Modified Capabilities

无。

## Impact

- 数据库：新增 `prompt_events` 表。
- 后端：上报与读取接口、存储方法、测试。
- 客户端：Claude Code 脚本与 settings 各加一处。
- 控制台：本轮不展示，先把数据取全。
