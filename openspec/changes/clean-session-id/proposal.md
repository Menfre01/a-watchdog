# Proposal

## Why

会话列表把存储键 `<key_id>:<标识串>` 原样显示。这个前缀在 Claude Code 里看不到：用户手里只有 UUID。前缀还会让筛选与复制都多一步。client key 维度已有独立的过滤参数，不需要挤进标识串。

## What Changes

- 会话列表与详情头部只显示客户端给的原始标识，不再带 `<key_id>:` 前缀。
- 新增「密钥」筛选下拉，选项来自已配置的 client key。该筛选可与标识前缀叠加。
- 存储格式不变。`session_id` 仍是 `<key_id>:<标识串>`,接口路径与请求参数继续用完整值。
- 后端修正叠加语义：同时给 `key_id` 与 `q` 时，前缀按 `<key_id>:<q>` 组合。此前 `key_id` 会覆盖 `q`。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `admin-console`: 「会话列表视图」明确标识显示为客户端原始值，密钥维度由独立筛选承担，并允许两者叠加。

## Impact

- 后端:`pkg/admin/traces.go` 的前缀组合逻辑与对应测试。
- 前端：会话列表的显示、筛选与详情头部。
- 存储与迁移：无改动。
