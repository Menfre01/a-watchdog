# Design

## Context

- 绿地：仓库仅含 AGENTS.md 与 OpenSpec 骨架；本变更首次落地 Go 模块、目录结构、Makefile 与构建链路。
- 行为契约见本变更 `specs/`（六个 capability）；动机与范围见 `proposal.md`，此处不重复。
- 关键外部约束（来自对 Claude Code / OpenAI / Anthropic 协议的前期调研，C1 必须从一开始满足）：
  - 流式请求必须逐块转发、不得缓冲；需原样转发 keep-alive / ping（SSE ping 与注释行）——Claude Code 在静默 300 秒后会中止流。
  - 上游错误体必须原样透传（客户端依赖错误原文做恢复逻辑）。
  - Anthropic 入向需按「开放列表」转发 `anthropic-*` 头与 body 扩展字段；客户端凭据可能出现在 `Authorization` 或 `x-api-key`。
  - 技术底座：单二进制、单实例、SQLite；运行时零 Node 依赖（前端仅构建期）。

## Goals / Non-Goals

**Goals:**
- 端到端最小骨架：OpenAI Chat 与 Anthropic Messages 两条同协议流式直通可用、可验收
- 责任链 + 配置/接入分离 + 控制台三个抽象就位，为 C2（轨迹）/ C3（IR 转换）/ C4（jev）留出插点
- 首次落地构建体系：Makefile、前端构建嵌入、表驱动测试基线

**Non-Goals:**
- 跨协议转换与 canonical IR、Responses 入向(C3);轨迹与 git 快照(C2);jev(C4)
- 条件路由、权重路由、配额与多用户
- 面向公网的安全加固(默认仅监听本机)

## Decisions

### D1 HTTP 层与流式转发（核心机制）

- 标准库 `net/http` + 标准路由；不引入 web 框架（中间件体系是自有责任链）。
- **响应方向:字节级转发 + 旁路观察**。响应体建立后进入转发循环(带 `Flush` 的分块拷贝),不做整体缓冲;同时以 tee 方式喂给旁路观察器做增量解析(提取 usage 等),观察器只读、解析失败不影响转发。
- **请求方向:解析-改写-重发**。JSON 解析为保留未知字段的结构(`map[string]any` + `json.Number` 防精度损失),仅将 `model` 替换为模型目录条目映射的上游模型名,再序列化发送。
- 超时:仅设置上游连接 / 首字节超时;流式响应不设整体超时;客户端断开即经 context 取消上游请求。
- 端点：`POST /v1/chat/completions`、`POST /v1/messages`（路径匹配，兼容 `?beta=true` 查询串）、`POST /v1/messages/count_tokens`、`/console/*`。

### D2 协议适配器

- 按「入向解析器 + 出向构造器」成对注册（protocol 名 → 适配器）：
  - inbound：识别路由键（`model`）、构造改写计划（模型名替换、头处理规则）
  - outbound：按 provider（base_url + api_key + protocol）构造上游请求；凭据头规则（OpenAI：`Authorization: Bearer`；Anthropic：`x-api-key` + 透传 `anthropic-version` / `anthropic-beta`）
- C1 实现 `openai-chat` 与 `anthropic-messages` 两对；接口形态为 C3 的 IR 转换预留（届时直通路径短路 IR）。
- 备选：为每个协议组合硬编码直通——放弃，C3 引入 IR 时会推翻。

### D3 责任链

- 固定四段：准入 → 路由/适配 → 调用 → 响应后处理；调用是链的终结点，"响应后处理"以 finalizer 方式环绕整条链，**保证被短路 / 被拒绝的请求也走记录**（对应 request-logging spec）。
- 中间件编译期注册（name → constructor），配置仅控制启用与参数；链在**配置加载 / 刷新时组装**，请求路径零反射。
- 短路：中间件返回受控错误即中止（不再进入路由 / 调用），finalizer 仍执行。
- 上下文：`RequestContext` 强类型结构（客户端身份、路由决策、上游元数据、记录字段）。
- 初始注册表：`auth`、`route`、`record`。

### D4 配置存储(SQLite)

- 驱动 `modernc.org/sqlite`(纯 Go);WAL 模式;启动时按 `PRAGMA user_version` 执行内嵌迁移;迁移文件保持 sqlc 可解析(以 DDL 为主,PRAGMA 放连接初始化)。
- 数据访问采用 **sqlc**(SQL-first 代码生成,非 ORM;生成代码基于 `database/sql`):schema 源 = 迁移文件,CRUD 与常用查询由 `.sql` 生成类型安全代码并入库;批量写入等特殊路径允许手写 SQL 兜底。
- 表(示意):
  - `providers(id, name, protocol, base_url, api_key, probe_model, enabled, created_at, updated_at)`
  - `models(id, name UNIQUE, provider_id → providers, upstream_model, enabled, created_at, updated_at)`
  - `client_keys(id, name, key_hash UNIQUE, key_prefix, enabled, created_at)`——存 SHA-256 哈希,明文仅在创建时一次性展示
  - `settings(key PRIMARY KEY, value)`——首版键：上游连接 / 首字节超时；为 C4 参数预留
  - `requests(id, ts, client_key_id, client_key_name, protocol, requested_model, provider_id, upstream_model, status_code, latency_ms, stream, usage_in, usage_out, usage_cache, error_kind)`
- 术语注记:「档位(effort)」指模型上的推理档位维度,由 C4(jev 选型)引入;C4 将在模型条目上新增「支持档位列表」,C1 仅做模型级路由
- 对应 spec:`config-store`。

### D5 快照与热刷新

- 进程内 `Snapshot`(providers / models / keys 索引 + settings),`atomic.Pointer` 原子替换;写路径(控制台 API)落库成功后重建并交换;请求路径只读快照。
- 鉴权用 key 哈希直查索引；停用即时生效（对应 `client-auth`）。

### D6 路由

- `route` 中间件按模型目录的客户端可见名精确匹配请求 `model`:命中 → 写入 `(provider, upstream_model)`;未命中 → 网关错误(OpenAI:404 + `error.code=model_not_found`;Anthropic:404 + `error.type=not_found_error`),不发起上游调用。
- 条目可见名允许与上游模型名同名,从而天然支持「写具体模型名」的直连用法。

### D7 控制台

- Vue 3 + TS + Vite 工程位于 `web/`；构建产物输出到 `pkg/admin/dist/`（Makefile 步骤），`go:embed all:dist` 嵌入；开发期可用 Vite dev server 代理 `/console/api`。
- Admin API：`/console/api/*` JSON REST（四类实体 CRUD + `POST /console/api/providers/{id}/test`）。
- 会话：口令来自 `--admin-password`（或环境变量）；登录签发 HMAC 签名的会话 cookie（HttpOnly、SameSite=Lax），变更类请求校验自定义头（CSRF 缓解）。
- 测试连接:以 provider 配置的「测试模型」发起 1-token 最小对话请求(协议对应的消息端点),一次验证地址、凭据与模型可用性;未配置测试模型时返回明确提示、不发起上游调用;失败须返回可读原因(含状态码与错误体片段)。
- 默认监听 `127.0.0.1`。

### D8 请求记录写入

- 有界缓冲 channel + 单独写协程（顺序 / 批量 INSERT）；请求路径只入队，非阻塞；队满丢弃并计数告警——**记录永不阻塞请求**（对应 `request-logging`）。
- usage 来源：非流式取响应体；流式由旁路观察器在事件流中提取（Anthropic `message_start` / `message_delta`；OpenAI 仅在客户端请求 `include_usage` 时存在），提取不到则置空。

### D9 工程与构建

- 目录:`cmd/ds-context`、`pkg/{chain,protocol,config,admin}`、`web/`;模块路径见 Open Questions。
- Makefile:`generate`(sqlc 代码生成)、`build`(前端构建 → `go build -trimpath`)、`run`、`test`、`lint`(golangci-lint 基线 + gofmt + `sqlc diff` 防生成漂移)、`clean`;产物 `bin/ds-context`。
- 依赖(全部有理由):`modernc.org/sqlite`(免 cgo,保单二进制);`github.com/google/go-cmp`(测试断言);构建期工具:前端工具链、`sqlc`(MIT,版本固定,golangci 排除生成代码)。
- 测试策略（TDD，覆盖率 ≥97%）：表驱动单测 + `httptest` 假上游；流式以「逐块到达、不被整体缓冲」为验证点（短超时读增量断言）；错误透传断言字节一致。

## Risks / Trade-offs

- [旁路观察器解析失败] → 只影响记录字段，不影响转发（结构隔离：观察器只读 tee，错误仅计数）→ 由对应单测锁定。
- [请求 body 改写引入差异] → 仅改 `model` 字段、保留其余原始结构（`map[string]any` + `json.Number`），单测锁定字段保真。
- [SSE 细节兼容] → 按调研清单实现（不缓冲 / ping 转发 / 注释行转发 / 错误不包裹），以 `httptest` 固定行为。
- [SQLite 并发写] → WAL + 单写者（记录写协程）；配置写入低频（控制台操作）。
- [控制台安全] → 默认仅本机监听 + 单口令会话 + CSRF 头校验；不承诺公网暴露场景。
- [上游探测不通用] → 「测试连接」失败原因需可读;必要时按协议降级探测路径。
- [sqlc 工具链与 SQLite 解析器] → 版本固定 + 生成代码入库 + `sqlc diff` 防漂移;解析器对复杂语法挑剔处用手写 SQL 兜底(混合模式)。

## Migration Plan

绿地首次落地，无迁移。首次启动自动创建数据文件与 schema；后续 schema 演进走 `user_version` 内嵌迁移。回滚 = 停止进程、删除数据文件（无外部状态）。

## Open Questions

- Go module 路径(是否挂远端仓库;未定前以 `ds-context` 为模块名,若后续需要变更则一次性替换 imports)
- ~~探测端点差异~~(已闭环:`/v1/models` 路径不可通用——DeepSeek 为 `/models`、`/anthropic` 前缀下各异——故改为最小对话请求 + provider 级「测试模型」字段,不依赖各家列表端点)
