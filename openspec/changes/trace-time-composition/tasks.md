# Tasks

- [x] 1.1 后端：骨架的每步附带 `tool_ms`,按发起的 step 归并工具事件时长。验证:`go test ./pkg/admin/` 通过。
- [x] 1.2 前端：统计卡新增时间构成条，三段合计与占比。验证:`pnpm typecheck` 通过。
- [x] 1.3 前端：步时间轴在模型条之后画工具段，步行文字加 `tool xx`。验证：含工具的步出现紫色段与文字。
- [x] 1.4 验证:`make test`、`make build` 通过。接口返回的步含 `tool_ms`。验证：实测四个步带工具时长。
