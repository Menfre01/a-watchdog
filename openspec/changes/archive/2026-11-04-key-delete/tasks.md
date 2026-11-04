# Tasks

## 1. 后端删除

- [x] 1.1 `pkg/config/queries/client_keys.sql` 新增删除查询并 `sqlc generate`。验证:`make generate` 后生成文件含删除方法。
- [x] 1.2 存储层新增删除方法，成功后刷新快照。不存在时返回 `ErrNotFound`。验证:`go test ./pkg/config/` 通过。
- [x] 1.3 新增 `DELETE /console/api/client-keys/{id}`,带 CSRF 与管理员校验。验证:`go test ./pkg/admin/` 通过，包含删除成功与 404 两条用例。

## 2. 前端

- [x] 2.1 模型目录表去掉 Provider 与上游模型名列，保留可见名、状态、操作。验证:`pnpm typecheck` 通过，页面只剩三列。
- [x] 2.2 密钥表去掉前缀列，保留名称、状态、操作。验证：页面只剩三列。
- [x] 2.3 密钥行新增删除按钮，打开确认弹窗。未完整输入名称时确认按钮禁用。验证：输入错误名称无法提交，输入正确名称后密钥从列表消失。

## 3. 验证

- [x] 3.1 `make test`、`pnpm typecheck`、`make build` 通过。验证：退出码 0。
- [x] 3.2 重启后删除一把测试密钥，用该密钥请求网关应被拒绝。验证：请求返回 401。
