# Tasks

## 1. 后端聚合

- [x] 1.1 `skeletonJSON` 按旁路标记分流累加，输出 `main` 与 `side` 两组 steps 与 in/out/cache。验证:`go test ./pkg/admin/` 通过。
- [x] 1.2 补测试：总计等于主路加旁路。无旁路时 side 为零值。验证:`make test` 通过。

## 2. 前端

- [x] 2.1 `api.ts` 的骨架类型补 `main` 与 `side`。验证:`pnpm typecheck` 通过。
- [x] 2.2 统计卡改为两行：总计行与拆分 行。旁路标注为子集。无旁路时只显示总计。验证：页面可见两行。

## 3. 验证

- [x] 3.1 `make test`、`pnpm typecheck`、`make build` 通过。验证：退出码 0。
- [x] 3.2 重启后打开含旁路的会话，确认总计等于主路加旁路。验证：数值对得上。
