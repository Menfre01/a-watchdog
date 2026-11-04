# Tasks

## 1. 存储与接口

- [x] 1.1 新增 0004 迁移:`tool_events` 表与唯一约束。验证:`make test` 中迁移版本断言更新为 4 并通过。
- [x] 1.2 新增查询与存储方法。`sqlc generate` 后生成产物入库。验证:`make lint` 的 `sqlc diff` 无差异。
- [x] 1.3 新增 `POST /v1/tool-events`:共用密钥鉴权、按 `client` 适配、幂等入库。验证:`go test ./pkg/server/` 通过。

## 2. 客户端接入文档

- [x] 2.1 新增 `docs/tool-events.md`,给出 Claude Code 的 settings 片段、Codex 的 command hook 示例与 DSH 插件说明。验证：文档含三份可直接复制的配置。
- [x] 2.2 说明时长口径：事件边界、含审批等待、缺失结束不推算。验证：文档与 `tool-timing` 规范一致。

## 3. 验证

- [x] 3.1 `make test`、`make build` 通过。验证：退出码 0。
- [x] 3.2 重启后用 curl 上报一对事件，再查表确认只有两条记录且可配对。验证：数据库查询结果正确。

## 4. 读取接口与控制台

- [x] 4.1 新增 `GET /console/api/tool-events?session_id=`,按 `tool_call_id` 配对。验证:`go test ./pkg/admin/` 通过。
- [x] 4.2 工具聚合面板新增「耗时(近似)」列,显示平均值与样本数,悬停给出最大值与审批标记。验证:`pnpm typecheck` 通过。
