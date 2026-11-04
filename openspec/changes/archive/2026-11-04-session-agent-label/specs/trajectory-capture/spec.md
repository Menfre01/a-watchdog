# Spec Delta

## MODIFIED Requirements

### Requirement: session 判定

系统 **必须(MUST)** 按**显式协议标识**判定快照归属的 session。Claude Code 取 `x-claude-code-session-id`，DSH 取 `x-deepseek-harness-session-id`，Codex 取 `session-id`/`session_id`/`thread-id`/`thread_id`。标识 **必须** 先经规范化（长度上限与字符白名单），不合规标识 **不得** 采信。未携带或不合规的请求 **必须** 归入按天分桶的匿名会话，**不得** 与显式会话混合。系统 **必须** 记录判定来源与判定理由：命中的头名、降级事件与匿名兜底随快照行落盘。判定 **必须** 按固定顺序取头：`x-claude-code-session-id`、`x-deepseek-harness-session-id`、`session-id`、`session_id`、`thread-id`、`thread_id`。Codex 同时给出会话标识与线程标识时，**必须** 取会话标识。判定 **必须** 同时给出 Agent 分类，取值于 Claude Code、Codex、DSH、Other。判定在捕捉时一次性写入，不提供在线重算。**链合兜底(按消息内容推断会话延续)不在本变更范围内**。

#### Scenario: 显式标识归组

- **WHEN** 一串请求携带相同的会话标识头
- **THEN** 它们被归入同一 session,判定来源标记为显式，判定理由记录命中的头名

#### Scenario: DSH 标识可判定

- **WHEN** 请求携带 `x-deepseek-harness-session-id`
- **THEN** 该请求被归入显式会话，Agent 分类为 DSH

#### Scenario: 压缩不断链

- **WHEN** 会话发生上下文压缩，新请求与历史快照不再构成前缀关系
- **THEN** 仍归入原 session,判定只依赖标识头，与消息内容无关

#### Scenario: 无标识归入匿名会话

- **WHEN** 请求未携带会话标识头
- **THEN** 它被归入按天分桶的匿名会话，Agent 分类为 Other

#### Scenario: 非法标识降级为匿名

- **WHEN** 会话标识头超长或含白名单外字符
- **THEN** 该值不采信为显式标识，降级事件留存于判定理由，请求按无标识处理

#### Scenario: 判定理由随行落盘

- **WHEN** 任一快照被写入
- **THEN** 其判定来源与理由随快照行持久化，误判可由理由直接排查，无需重放判定
