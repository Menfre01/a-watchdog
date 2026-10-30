# client-auth

## Purpose

定义客户端访问网关的凭据校验行为:凭据载体、通过/拒绝语义,以及密钥生命周期的即时生效约定。由此保证网关对客户端身份的判定即时、一致,且每次请求都能归因到具体的密钥。

## Requirements

### Requirement: 凭据载体与校验

系统 **必须(MUST)** 接受两种客户端凭据载体:`Authorization: Bearer <key>` 与 `x-api-key: <key>`(二者取其一或同时提供均可);携带有效且启用 key 的请求放行并继续后续处理。

#### Scenario: Bearer 凭据通过

- **WHEN** 请求携带 `Authorization: Bearer <有效 key>`
- **THEN** 请求通过鉴权并继续处理

#### Scenario: x-api-key 凭据通过

- **WHEN** 请求仅携带 `x-api-key: <有效 key>`
- **THEN** 请求通过鉴权并继续处理

### Requirement: 拒绝语义

缺少凭据、凭据无效或凭据已停用时,系统 **必须(MUST)** 拒绝请求并返回 401;错误响应体 **必须** 符合入向协议对应的错误格式。

#### Scenario: 无效 key

- **WHEN** Anthropic 协议请求携带已停用的 key
- **THEN** 返回 401,响应体为 Anthropic 协议风格的鉴权错误结构

#### Scenario: 缺少凭据

- **WHEN** OpenAI 协议请求未携带任何凭据头
- **THEN** 返回 401,响应体为 OpenAI 协议风格的错误结构

### Requirement: 密钥生命周期即时生效

密钥的新增与停用 **必须(MUST)** 即时生效:停用后到达的新请求 **必须** 立即被拒绝,无需重启进程。

#### Scenario: 停用后立即生效

- **WHEN** 某 key 被停用
- **THEN** 使用该 key 的后续请求立即收到 401
