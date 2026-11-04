# Tasks

## 1. 工程骨架与构建

- [x] 1.1 初始化 Go 模块(模块名暂定 `awatchdog`)与目录骨架 `cmd/awatchdog`、`pkg/{chain,protocol,config,admin}`、`web/`,验证:`go build ./...` 通过
- [x] 1.2 落地 Makefile(`generate`/`build`/`run`/`test`/`lint`/`clean`,build 串联前端构建),验证:空骨架下 `make build`、`make lint`、`make clean` 可用
- [x] 1.3 配置 golangci-lint 基线(gosec/errcheck/errname/govet/staticcheck/ineffassign/unused/unconvert/copyloopvar/noctx)与 gofmt 检查,验证:`make lint` 干净通过(工具未安装时给出指引,不调用 go install)
- [x] 1.4 引入依赖 `modernc.org/sqlite` 与 `github.com/google/go-cmp`(理由已记于 design.md),验证:`go mod tidy` 后 `make build` 通过
- [x] 1.5 接入 sqlc(sqlc.yaml v2、版本固定;schema 源指向迁移目录;生成代码入库;`make generate` 幂等;lint 串 `sqlc diff` 防漂移;golangci 排除生成代码),验证:`make generate` 后 `sqlc diff` 干净

## 2. 责任链框架(request-pipeline)

- [x] 2.1 定义强类型 `RequestContext` 与四阶段枚举(准入/路由适配/调用/响应后处理),验证:类型与阶段单测通过
- [x] 2.2 实现中间件注册表与链组装(编译期注册;配置启停与参数;未注册名字加载报错),验证:组链表驱动测试(启用/禁用/未知名报错)
- [x] 2.3 实现短路语义与 finalizer 环绕(短路跳过路由/调用、finalizer 仍执行),验证:短路行为单测通过

## 3. 配置存储(config-store)

- [x] 3.1 SQLite 打开与内嵌迁移(WAL、`user_version`),验证:首次建库与重复启动幂等单测通过
- [x] 3.2 schema 与 CRUD(sqlc 生成):providers / models(模型目录)/ client_keys(SHA-256 哈希 + 前缀)/ settings,验证:四实体 CRUD 表驱动测试(含唯一约束、provider 引用)
- [x] 3.3 快照构建与原子刷新(`atomic.Pointer`;写后重建交换),验证:写后新视图、并发读写安全测试通过
- [x] 3.4 requests 表写入接口(供记录环节;批量写入手写 SQL 兜底;字段口径按 spec),验证:插入与字段完整性单测通过

## 4. 协议适配与流式转发(protocol-routing)

- [x] 4.1 适配器接口与注册(inbound 解析 / outbound 构造成对;为 C3 IR 预留形态),验证:接口一致性单测通过
- [x] 4.2 上游 HTTP 客户端与流式转发通用件(分块 Flush 转发循环、tee 旁路观察器、连接/首字节超时、context 取消),验证:转发不缓冲与取消传播单测通过
- [x] 4.3 OpenAI Chat 链路(`/v1/chat/completions`,流式+非流式;`model` 改写;凭据替换),验证:httptest 假上游端到端单测通过
- [x] 4.4 Anthropic 链路(`/v1/messages` 路径匹配含 `?beta=true`、`count_tokens`;`anthropic-*` 开放列表转发),验证:httptest 端到端单测 + 头部转发断言通过
- [x] 4.5 错误与响应形状(上游错误字节透传不包裹;网关自身错误按入向协议形状),验证:字节一致性 + 两协议错误结构断言通过
- [x] 4.6 头卫生(客户端 key 不出站、出站头白名单),验证:出站请求头断言测试通过

## 5. 链上环节(auth / route / record)

- [x] 5.1 auth 环节(`Authorization: Bearer` 与 `x-api-key` 双载体、哈希快照校验、401 协议形状),验证:双协议 × 有效/无效/停用/缺头表驱动测试通过
- [x] 5.2 route 环节(模型目录精确匹配、决策写入上下文、未命中协议形状错误且不发起上游),验证:路由单测通过
- [x] 5.3 record finalizer(有界队列 + 写协程、非阻塞、被拒请求也记录、usage 提取对接),验证:非阻塞与字段口径测试通过

## 6. 服务装配与端到端

- [x] 6.1 main 装配(flags/env、存储+快照+链+端点注册、graceful shutdown、默认监听 127.0.0.1),验证:启动冒烟测试通过
- [x] 6.2 端到端验收测试(httptest 假上游:配置 → key → 流式请求 → 上游直通 → 记录可见),验证:测试通过
- [x] 6.3 真实上游手动冒烟(配置一个真实 provider 用 curl/OpenAI SDK 流式验证;可选 Claude Code 指向网关冒烟),验证:验收清单勾选、请求记录正确

## 7. 控制台(admin-console)

- [x] 7.1 Admin API 鉴权(口令登录、HMAC 会话 cookie、CSRF 头校验、未认证拒绝),验证:httptest 鉴权测试通过
- [x] 7.2 配置 CRUD API(四实体,写后即时生效),验证:API 测试(写后请求行为变化)通过
- [x] 7.3 测试连接 API(以 provider 配置的「测试模型」发起 1-token 最小请求;未配置时明确提示;可读失败原因含状态码与错误体),验证:假上游成功/失败/未配置用例通过
- [x] 7.4 前端脚手架(Vue 3 + TS + Vite:登录页与基础布局),验证:`pnpm build` 产物生成、dev server 可访问
- [x] 7.5 配置管理页面(providers / models / client_keys / settings + 测试连接按钮),验证:构建通过 + 手动可用性清单
- [x] 7.6 嵌入集成(`go:embed all:dist`、Makefile 构建顺序),验证:`make build` 后单二进制访问 `/console` 可用

## 8. 收尾与验收

- [x] 8.1 覆盖率核查 ≥97%(不可模拟路径显式排除并记录),验证:`go test ./... -cover` 报告核对
- [x] 8.2 全量 `make lint` 与 `make test` 通过,验证:命令输出干净
- [x] 8.3 最小使用说明(README:启动、配置、验收路径),验证:按 README 从零走通一次
- [x] 8.4 按 AGENTS.md 发起代码审查并反馈结论,验证:审查报告产出(逻辑正确性/边界/安全)
