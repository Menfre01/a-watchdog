# Tasks

## 1. 存储与接口

- [x] 1.1 新增 0005 迁移:`prompt_events` 表与有 id 时的唯一索引。验证：迁移版本断言更新为 5 并通过。
- [x] 1.2 新增查询与存储方法,`sqlc generate` 后产物入库。验证:`go test ./pkg/config/` 通过。
- [x] 1.3 新增 `POST /v1/prompt-events`:复用密钥鉴权，只存时刻与长度。验证:`go test ./pkg/server/` 通过。
- [x] 1.4 新增 `GET /console/api/prompt-events?session_id=`。验证:`go test ./pkg/admin/` 通过。

## 2. 客户端

- [x] 2.1 转发脚本按 `hook_event_name` 分流。验证：手工回放 `UserPromptSubmit` 载荷后表中有记录。
- [x] 2.2 `settings.json` 增加 `UserPromptSubmit` 钩子。验证:`hooks` 含四个事件名。

## 3. 验证

- [x] 3.1 `make test`、`make build` 通过。验证：退出码 0。
- [x] 3.2 重启后确认：提示提交与工具事件分别落到两张表。验证：两次上报各产生一条记录。
