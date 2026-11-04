# Proposal

## Why

会话列表把判定来源与理由原样显示。`explicit: x-claude-code-session-id` 这类文本对管理员没有用：他要知道的是「这是哪个 agent 的会话」，不是判定过程。会话标识也是整串 UUID,列表里占地方。

## What Changes

- 会话列表的「判定」列改为 **Agent 分类**:Claude Code、Codex、DSH、Other 四类。
- 会话标识只显示短前缀(8 位加省略号),完整值放 `title`。轨迹头部保留完整标识。
- 逐条判定理由不再显示，数据仍随快照保留在库中。
- 会话判定新增 DSH 的标识头 `x-deepseek-harness-session-id`。此前这类请求都落到匿名会话。
- 列表接口新增 `agent` 字段，由判定理由派生，不新增数据库列。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `admin-console`: 「会话列表视图」的判定列改为 Agent 分类，标识改为短前缀显示。
- `trajectory-capture`: 「session 判定」增加 DSH 方向的显式标识头，判定结果同时给出 agent 分类。

## Impact

- 后端:`pkg/trace/session.go` 的头名列表与分类函数,`pkg/admin/traces.go` 的列表 JSON。
- 前端：可观测会话列表的列与显示函数。
- 存储：不新增列，不迁移。
