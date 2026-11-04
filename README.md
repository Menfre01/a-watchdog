# AWatchDog

个人 / 小团队自托管的 LLM 网关与上下文轨迹分析服务。单二进制，内嵌控制台，配置与接入分离。

- 入向协议：OpenAI Chat Completions（`POST /v1/chat/completions`）与 Anthropic Messages（`POST /v1/messages`，含 `count_tokens`）
- 出向：同协议直通（OpenAI 兼容上游 / Anthropic 原生上游），流式逐块转发不缓冲
- 控制台：两个模块。「可观测」看会话、轨迹、工具与请求指标。「配置」管接入、模型目录与客户端密钥。
- 请求记录：每请求一条（含网关拒绝的请求），异步落库不阻塞请求
- 轨迹捕捉：异步保存请求与响应原文，创建规范化快照供分析
- 事件上报：工具与提示事件由客户端上报。工具事件用于还原调用耗时。

> 接入与配置的全流程故事见 `docs/user-story.md`（C1 与 C2 补充）。行为规范在 `openspec/specs/`。

## 构建

前置：Go 1.25+、[pnpm](https://pnpm.io/) 与 [sqlc](https://sqlc.dev/) v1.27+。

```bash
make generate   # 从 SQL 创建数据访问代码（已入库。改 SQL 后必须重新运行）
make build      # 构建前端，编译单二进制到 bin/awatchdog
make test       # 全部测试
make lint       # gofmt + golangci-lint + sqlc diff（防代码漂移）
make coverage   # 按包输出覆盖率（目标见 AGENTS.md）
```

## 运行

```bash
./bin/awatchdog \
  --listen 127.0.0.1:8399 \
  --data-dir ./.awatchdog \
  --admin-password '你的口令'
```

仓库开发时数据目录固定在 `./.awatchdog`。该目录已在 `.gitignore` 中忽略。

| 参数 | 默认 | 说明 |
|---|---|---|
| `--listen` | `127.0.0.1:8399` | 监听地址。默认仅本机。loopback 同时监听 IPv4 与 IPv6。 |
| `--data-dir` | `~/.awatchdog`（仓库开发用 `./.awatchdog`） | 数据目录（SQLite 数据文件）。 |
| `--admin-password` | 环境变量 `AWATCHDOG_ADMIN_PASSWORD` | 控制台管理员口令。未提供时控制台登录不可用。 |

## 快速上手

1. 打开控制台 `http://127.0.0.1:8399/console`，用管理员口令登录。
2. **接入**：创建 provider。填协议（`anthropic-messages` 或 `openai-chat`）、Base URL、API Key 与测试模型。点「测试连接」验证。
3. **模型目录**：创建「客户端可见名 → provider + 上游模型名」的映射。
4. **客户端密钥**：创建 key。明文只显示一次。
5. 接入客户端。例如 Claude Code：

   ```bash
   export ANTHROPIC_BASE_URL=http://127.0.0.1:8399
   export ANTHROPIC_AUTH_TOKEN=awd_xxxxxxxx    # 控制台创建的 key
   claude
   ```

   OpenAI SDK 的 `base_url` 指向 `http://127.0.0.1:8399/v1`。`api_key` 填网关 key。

6. 发一次请求，打开控制台「可观测」查看会话与轨迹。数据文件中的 `requests` 表也可直接查。

换上游只需在配置区改模型目录指向的 provider。客户端无需变动。

## 可观测

「可观测」模块只读。它不改请求记录与轨迹，也不写派生数据。

- **会话列表**：按密钥与标识前缀过滤。每行给出 Agent 分类、首末时间、step 数与累计 token。分类来源见 `docs/session-attribution.md`。
- **轨迹**：按轮次与 step 展开。逐段显示请求、响应与工具调用。推断项与实测项分开标注。
- **工具**：会话级聚合。统计调用次数、状态与发起轮次。可按工具名、状态与来源筛选。
- **请求指标**：请求数、错误率、p50 与 p95 延迟、缓存命中率、token 输入与输出。窗口可选 1 小时、24 小时、7 天与全部。
- **事件上报**：工具与提示事件经客户端上报。工具耗时的口径与配置示例见 `docs/tool-events.md`。
- **形态基准**：轨迹视图以 `docs/trail-view.html` 为准。

## 目录结构

```
cmd/awatchdog/    单二进制入口（网关与控制台装配）
pkg/chain/         责任链框架（固定阶段序、强类型上下文、短路）
pkg/protocol/      协议适配（解析、构造、流式转发、错误形状）与上游 Proxy
pkg/middleware/    链上环节：auth（鉴权）、route（路由）、record（请求记录与轨迹捕捉）
pkg/trace/         轨迹规范化、轮次与链推断、diff、工具抽取
pkg/config/        SQLite 持久化（迁移、快照、CRUD。db/ 由 sqlc 产出）
pkg/admin/         控制台管理面（会话、CRUD API、可观测查询、连通性测试、内嵌前端）
pkg/server/        责任链装配（Wire）、HTTP 入口与事件端点
web/               控制台前端源码（Vue 3 + TS + Vite。产物 embed 进二进制）
docs/              使用文档
scripts/           文字检查与原型冒烟脚本
```

## 开发注记

- 责任链阶段固定为：响应后处理（finalizer）→ 准入 → 路由/适配 → 上游调用。中间件编译期注册，启用与参数由配置控制。
- 记录与轨迹捕捉在响应后处理环节异步运行。队列满时丢弃并计数，不阻塞请求。
- 数据库访问由 sqlc 产出。特殊路径（批量写记录）手写 SQL。改 `.sql` 后必须 `make generate`，否则 `make lint` 的 `sqlc diff` 会失败。
- SQL 查询文件保持纯 ASCII。sqlc 解析器对多字节字符与占位符混排存在缺陷。
- 上游错误原样透传（不加网关错误信封）。流式响应不缓冲、转发 ping。这是兼容 Claude Code 的硬约束。
- 轨迹分析只读。新增视图不得写入派生数据。
- 待定问题与样本收集见 `docs/open-questions.md`。
