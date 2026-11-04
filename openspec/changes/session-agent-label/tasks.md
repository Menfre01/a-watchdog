# Tasks

## 1. 判定与分类

- [x] 1.1 `pkg/trace/session.go` 增加 DSH 头名与 `Agent` 字段，并提供由判定理由派生的分类函数。验证:`go test ./pkg/trace/` 通过。
- [x] 1.2 `pkg/admin/traces.go` 的列表 JSON 增加 `agent` 字段。验证:`go test ./pkg/admin/` 通过，含 agent 断言。

## 2. 列表显示

- [x] 2.1 `web/src/api.ts` 的会话类型增加 `agent`。列表「判定」列改为 Agent 分类徽标。验证:`pnpm typecheck` 通过。
- [x] 2.2 列表标识改为前 8 位加省略号，完整值进 `title`。详情头部保持完整值。验证：列表不再铺满整列，悬停可见完整标识。

## 3. 验证

- [x] 3.1 `make test`、`pnpm typecheck`、`make build` 通过。验证：退出码 0。
- [x] 3.2 重启后确认：Claude Code 会话显示 Claude Code 分类，标识为短前缀。验证：页面可见。
