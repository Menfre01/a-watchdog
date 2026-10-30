# ds-context

个人 / 小团队自托管的 LLM 网关:单二进制,内嵌控制台,配置与接入分离。

- 入向协议:OpenAI Chat Completions(`POST /v1/chat/completions`)与 Anthropic Messages(`POST /v1/messages`,含 `count_tokens`)
- 出向:同协议直通(OpenAI 兼容上游 / Anthropic 原生上游),流式逐块转发不缓冲
- 控制台:providers(接入)/ 模型目录 / 客户端密钥 / 全局配置 的 CRUD 与连通性测试
- 请求记录:每请求一条(含被拒绝请求),异步落库不阻塞请求

> 完整使用路径见 `docs/user-story.md`;行为规范在 `openspec/specs/`(随变更归档生成)。

## 构建

前置:Go 1.25+、[pnpm](https://pnpm.io/)、[sqlc](https://sqlc.dev/) v1.27+。

```bash
make generate   # 从 SQL 生成数据访问代码(已入库;改动 SQL 后重新生成)
make build      # 构建前端 → 编译单二进制到 bin/ds-context
make test       # 全部测试
make lint       # gofmt + golangci-lint + sqlc diff(防生成漂移)
```

## 运行

```bash
./bin/ds-context \
  --listen 127.0.0.1:8399 \
  --data-dir ~/.ds-context \
  --admin-password '你的口令'
```

| 参数 | 默认 | 说明 |
|---|---|---|
| `--listen` | `127.0.0.1:8399` | 监听地址(默认仅本机;loopback 同时监听 IPv4 与 IPv6) |
| `--data-dir` | `~/.ds-context` | 数据目录(SQLite 数据文件) |
| `--admin-password` | 环境变量 `DS_CONTEXT_ADMIN_PASSWORD` | 控制台管理员口令;未设置则控制台登录不可用 |

## 快速上手

1. 打开控制台 `http://127.0.0.1:8399/console`,用管理员口令登录;
2. **接入**:新建 provider(协议 `anthropic-messages` 或 `openai-chat`、Base URL、API Key、测试模型),点「测试连接」验证(以测试模型发一条 1-token 最小请求);
3. **模型目录**:建立「客户端可见名 → provider + 上游模型名」的映射;
4. **客户端密钥**:生成 key(明文仅展示一次);
5. 接入客户端——例如 Claude Code:

   ```bash
   export ANTHROPIC_BASE_URL=http://127.0.0.1:8399
   export ANTHROPIC_AUTH_TOKEN=dsctx_xxxxxxxx    # 控制台生成的 key
   claude
   ```

  或 OpenAI SDK:`base_url = http://127.0.0.1:8399/v1`,`api_key` 填网关 key。
6. 请求完成后,数据文件中 `requests` 表即可看到请求记录。

换上游只需在控制台改模型目录指向的 provider,客户端无需变动。

## 目录结构

```
cmd/ds-context/    单二进制入口(网关 + 控制台装配)
pkg/chain/         责任链框架(固定阶段序、强类型上下文、短路)
pkg/protocol/      协议适配(解析/构造/流式转发/错误形状)与上游 Proxy
pkg/middleware/    链上环节:auth(鉴权)/ route(路由)/ record(请求记录)
pkg/config/        SQLite 存储(迁移、快照、CRUD;db/ 为 sqlc 生成代码)
pkg/admin/         控制台管理面(会话、CRUD API、连通性测试、内嵌前端)
pkg/server/        责任链装配(Wire)与 HTTP 入口
web/               控制台前端源码(Vue 3 + TS + Vite,产物 embed 进二进制)
docs/              使用文档
```

## 开发注记

- 责任链阶段固定为:响应后处理(finalizer)→ 准入 → 路由/适配 → 上游调用;中间件编译期注册,启用与参数由配置控制。
- 数据库访问由 sqlc 生成;特殊路径(批量写记录)手写 SQL。改 `.sql` 后必须 `make generate`,否则 `make lint` 的 `sqlc diff` 会失败。
- SQL 查询文件保持纯 ASCII(sqlc 解析器对多字节字符与占位符混排存在缺陷)。
- 上游错误原样透传(不加网关错误信封);流式响应不缓冲、转发 ping——这是兼容 Claude Code 的硬约束。
