# 会话归属与 Agent 分类

本文说明控制台会话列表里的标识与分类从哪来。依据是各客户端源码与官方变更记录，不是猜测。

## 存储格式

`session_id` 在库里是 `<client_key_id>:<客户端标识>`,冒号前是网关的密钥 id,冒号后是客户端给的标识。控制台只显示后半段。

## 判定顺序

网关按下列顺序取第一个存在的头。带厂商前缀的头排在通用拼写之前，避免被通用头抢先。

| 顺序 | 头名 | 来源 | Agent 分类 |
|---|---|---|---|
| 1 | `x-claude-code-session-id` | Claude Code v2.1.86 起发送 | Claude Code |
| 2 | `x-deepseek-harness-session-id` | DSH 的 DeepSeek 适配器 | DSH |
| 3 | `session-id` | Codex(连字符拼写) | Codex |
| 4 | `session_id` | Codex(下划线拼写) | Codex |
| 5 | `thread-id` | Codex(连字符拼写) | Codex |
| 6 | `thread_id` | Codex(下划线拼写) | Codex |

都没有，或标识不合规(超长、含白名单外字符)时，归入按天分桶的匿名会话，分类为 Other。

## 依据

- **Claude Code**:v2.1.86 变更记录写明新增 `X-Claude-Code-Session-Id`,目的是让代理按会话聚合请求而无需解析请求体。相关讨论见 [anthropics/claude-code#40119](https://github.com/anthropics/claude-code/issues/40119)。
- **Codex**:`codex-api` 的 `build_session_headers` 同时写入下划线与连字符两种拼写，即 `session_id`/`session-id` 与 `thread_id`/`thread-id`。见 [openai/codex#21757](https://github.com/openai/codex/pull/21757) 的差异。
- **DSH**:`dsh-llm-deepseek` 在带 `sessionId` 选项时发送 `x-deepseek-harness-session-id`。

## 为什么 session 排在 thread 之前

Codex 同一次请求里同时带 `session_id` 与 `thread_id`。会话是更高一层：一个会话内可以有多个线程。取 `session_id` 才能把同一会话的分支请求聚在一起。取 `thread_id` 会把会话拆散。

## 已知边界

- **DSH 头只出现在 DeepSeek 适配器**。DSH 若经其他协议访问网关，可能不带这个头，会话会落到 Other。要覆盖这种情况，需要那个适配器也发送同名头。
- **判定在捕捉时写入**。历史会话不回算。新增或调整头名只影响之后的请求。
- **未登记的头一律归 Other**。要支持新客户端，得在判定表里加一行，并补一条分类用例。
- **分类不是鉴权**。客户端可以伪造这些头，分类只用于阅读，不用于安全判断。
