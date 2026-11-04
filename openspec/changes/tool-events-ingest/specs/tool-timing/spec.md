# Spec Delta

## Purpose

把客户端上报的工具调用事件变成可查询的调用记录。记录标明每次调用何时开始、何时结束、来自哪个客户端。时长口径如实标注。

## ADDED Requirements

### Requirement: 工具事件上报端点

网关 **必须(MUST)** 提供 `POST /v1/tool-events`。来源 **必须** 由 `client` 查询参数区分，取值 `claude-code`、`codex` 或 `dsh`。

请求 **必须** 用客户端密钥鉴权。凭据载体与网关其它端点一致，取 `Authorization: Bearer` 或 `x-api-key`。未通过鉴权 **必须** 返回未授权，且 **不得** 落库。

服务端 **必须** 把各客户端载荷适配为统一事件。事件含**会话标识**、**轮次标识**（可选）、**工具调用标识**、**工具名**、**阶段**（`start` 或 `end`）与**状态**。缺少会话标识或工具调用标识的请求 **必须** 拒绝。

#### Scenario: 鉴权失败不落库

- **WHEN** 未携带有效客户端密钥上报工具事件
- **THEN** 返回未授权，且不产生任何事件记录

#### Scenario: Claude Code 载荷可适配

- **WHEN** `client=claude-code` 上报 `hook_event_name` 为 `PreToolUse` 的事件
- **THEN** 事件以 `start` 阶段落库，工具调用标识取自 `tool_use_id`

#### Scenario: 失败事件

- **WHEN** Claude Code 上报 `PostToolUseFailure`
- **THEN** 事件以 `end` 阶段与 `error` 状态落库

### Requirement: 事件幂等

同一客户端的同一工具调用、同一阶段 **必须(MUST)** 只落一条记录。重复上报 **不得** 产生重复行，也 **不得** 改写既有记录。

#### Scenario: 重复上报被忽略

- **WHEN** 同一 `tool_use_id` 的 `end` 事件被上报两次
- **THEN** 表中只有一条 `end` 记录

### Requirement: 时长的口径与标注

工具调用时长 **必须(MUST)** 由同一工具调用标识的 `start` 与 `end` 事件配对得出。时长取两个事件到达网关时间的差。

每条记录 **必须** 标注边界类型与审批等待。边界类型 `hook` 表示事件边界。没有 `end` 事件的调用 **不得** 推算时长，状态 **必须** 标记为未完成。

界面或接口给出时长时 **必须** 说明它包含 hook 分发与网络开销。含审批等待时 **不得** 把它当作纯执行时间。

#### Scenario: 成对事件得到时长

- **WHEN** 同一工具调用先上报 `start` 再上报 `end`
- **THEN** 该调用有一条时长记录，边界类型为 `hook`

#### Scenario: 缺少结束事件

- **WHEN** 某个工具调用只有 `start` 事件
- **THEN** 该调用显示为未完成，不给出时长

### Requirement: 工具调用读取接口

控制台 **必须(MUST)** 提供 `GET /console/api/tool-events?session_id=<会话标识>`。接口 **必须** 按 `tool_call_id` 把开始与结束事件配对后返回。返回项含工具名、开始时间、结束时间、时长、状态、边界类型、审批标记与完成标记。

缺少 `session_id` 的请求 **必须** 返回 400。只有开始事件的调用 **必须** 返回 `complete=false` 且不给出时长。

#### Scenario: 成对调用带时长

- **WHEN** 某会话有一条完整的开始与结束事件
- **THEN** 读取接口返回一条 `complete=true` 的调用,并给出时长

#### Scenario: 缺结束不推算

- **WHEN** 某调用只有开始事件
- **THEN** 读取接口返回 `complete=false`,且不含时长字段

#### Scenario: 缺参数被拒

- **WHEN** 请求未带 `session_id`
- **THEN** 返回 400
