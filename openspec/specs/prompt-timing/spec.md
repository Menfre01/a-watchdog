# prompt-timing Specification

## Purpose
采集客户端提交提示的时刻，让轨迹能算出人从看到结果到再次输入之间花了多久。

## Requirements

### Requirement: 提示提交上报

网关 **必须(MUST)** 提供 `POST /v1/prompt-events?client=<claude-code|codex|dsh>`。鉴权 **必须** 与工具事件端点一致，未通过鉴权 **不得** 落库。

事件 **必须** 记录会话标识、提交时刻与提示长度。提示正文 **不得** 落库。

同一客户端、同一会话、同一 `prompt_id` **必须** 只落一条。缺少 `prompt_id` 时按每条一次记录。

#### Scenario: 上报成功

- **WHEN** Claude Code 提交提示并上报 `UserPromptSubmit`
- **THEN** 系统记录一条提交事件（会话、时刻、提示长度），不保存正文

#### Scenario: 重复上报被忽略

- **WHEN** 同一 `prompt_id` 上报两次
- **THEN** 表中只有一条记录

#### Scenario: 鉴权失败

- **WHEN** 请求未携带有效客户端密钥
- **THEN** 返回未授权，且不落库

### Requirement: 提示提交读取

控制台 **必须(MUST)** 提供 `GET /console/api/prompt-events?session_id=<会话标识>`，返回该会话的提交事件，按时间升序。

缺少 `session_id` **必须** 返回 400。

#### Scenario: 按会话读取

- **WHEN** 管理员按会话读取提交事件
- **THEN** 返回该会话的全部提交时刻，按时间升序

#### Scenario: 缺参数被拒

- **WHEN** 请求未带 `session_id`
- **THEN** 返回 400
