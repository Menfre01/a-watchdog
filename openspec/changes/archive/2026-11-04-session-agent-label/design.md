# Design

## Context

见 `proposal.md` 的 Why。快照行已保存判定来源与理由(`session_source`、`session_note`),理由形如 `explicit: x-claude-code-session-id`。DSH 发送 `x-deepseek-harness-session-id`,当前不在头名列表里，这类请求全部落到匿名会话。

## Goals / Non-Goals

**Goals:**

- 列表一眼看出会话属于哪个 agent。
- 标识短、可悬停看全。
- DSH 会话可显式判定。

**Non-Goals:**

- 不改快照表结构，不加列。
- 不提供按 Agent 过滤(本轮只改显示。要过滤再加参数)。
- 不回溯重算历史判定。

## Decisions

### 1. 分类由判定理由派生

`AgentFromNote` 读 `session_note` 的头名映射到四类：Claude Code、DSH、Codex、Other。理由：理由已经落盘且不含歧义，派生不需要迁移，也不需要重算。

备选是新增 `agent` 列。它每次判定都要写入，回填历史也需要一次迁移，而收益只是少一次字符串解析。

### 2. 头名顺序

`x-claude-code-session-id` → `x-deepseek-harness-session-id` → `session-id` → `session_id` → `thread-id` → `thread_id`。Claude Code 与 DSH 的头名带前缀，不会与通用名混淆。Codex 的四种拼写放最后。

### 3. 列表短前缀，详情看全

列表显示原始标识前 8 位加省略号，完整值进 `title`。轨迹头部继续显示完整标识。理由：列表是扫描用途，详情才是核对用途。

## Risks / Trade-offs

- **历史 DSH 会话已是匿名** → 判定在捕捉时写入，不回算。新请求才按 DSH 分类。这与既有「不提供在线重算」的约定一致。
- **Agent 分类只认头名** → 客户端换头名会落到 Codex 或 Other。需要时扩展映射表。
- **短前缀可能重复** → 只用于显示，筛选与详情仍按完整值。

## Migration Plan

无迁移。回滚为撤销头名与字段，前端回退判定列。
