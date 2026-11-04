# Proposal

## Why

为个人/小团队自用构建一个 LLM 网关的主体框架：以责任链承载「鉴权 → 协议路由 → 日志」等可扩展环节，配置与接入分离并由控制台统一管理。它同时是两根支柱的底座——上下文轨迹（C2）与 jev 智能选型（C4）。仓库当前无代码，本变更先落地一个端到端可跑通、可独立验收的最小骨架。

## What Changes

- **责任链运行框架**:中间件编译期注册(名字 + 构造),链的启用与参数由配置控制;固定阶段序(准入 → 路由/适配 → 调用 → 响应后处理);强类型请求上下文逐层填充,环节间不靠弱类型 map 传递
- **配置与接入分离(存储)**:SQLite 单文件,独立表 `providers`(接入:protocol / base_url / api_key / 启用)与 `models`(模型目录)、`client_keys`、`settings`(配置);内存快照、写库即刷新,请求路径不读库
- **管理控制台 v1(内嵌单二进制)**:providers / models / client_keys / settings 的配置 CRUD、「测试连接」(按协议发最轻量上游请求)、单管理员口令 + session;前端 Vue 3 + TS + Vite,构建产物 `go:embed`
- **客户端鉴权**：控制台生成的静态 Bearer key（命名、可停用），鉴权中间件查内存快照
- **协议服务面（两条同协议直通）**：
  - OpenAI Chat Completions 入向 → OpenAI 兼容上游直通（含 OpenAI 官方）
  - Anthropic Messages 入向 → Anthropic 原生上游直通（含 `count_tokens` 透传）
  - 流式硬约束:不缓冲、转发 keep-alive/ping、上游错误原文透传不包裹;`anthropic-*` 头与 body 字段按开放列表原样转发
- **路由**:请求 `model` 命中模型目录条目 → 路由到该条目的 provider+上游模型;未命中 → 返回明确的未路由错误(不静默转发)。模型目录即路由的最小形态,条件路由后置
- **请求日志**:每请求一条记录落 SQLite(时间、client key、入向协议、请求模型、实际 provider/model、状态码、延迟、usage、流式标记)
- **构建体系**:Makefile(generate / build / test / lint / run / clean,含前端构建与 sqlc 代码生成);目录骨架 `cmd/awatchdog` + `pkg/{chain,protocol,config,admin}` + `web`
- **范围外（后续变更）**：轨迹系统（session 判定 / git 快照 / 压缩分析）为 C2；OpenAI Responses 入向、跨协议 IR 转换为 C3；jev 决策与选择器为 C4

## Capabilities

### New Capabilities

- `request-pipeline`: 责任链运行框架——中间件注册、固定阶段序、类型化请求上下文、按配置启用与传参
- `protocol-routing`: 入向协议服务与出向直通(OpenAI Chat / Anthropic Messages)、模型目录路由、未命中错误、流式保真(不缓冲 / ping 转发 / 错误不包裹)
- `client-auth`: 客户端静态 Bearer key 鉴权(生成、停用、快照校验)
- `config-store`: 接入/配置分离的存储模型、内存快照与刷新语义
- `admin-console`: 内嵌控制台——配置 CRUD、测试连接、管理员口令鉴权
- `request-logging`: 请求记录持久化与字段口径（含 usage / 延迟 / 状态）

### Modified Capabilities

<!-- 无：绿地项目，尚无既有 specs 需要修改 -->

## Impact

- 全新代码与目录：`cmd/awatchdog`、`pkg/{chain,protocol,config,admin}`、`web/`、`Makefile`（首次落地构建入口）
- 新增第三方依赖及理由:
  - `modernc.org/sqlite`:纯 Go、免 cgo,保住静态单二进制分发
  - 构建期工具 `sqlc`(MIT,SQL-first 代码生成):仅构建期、无运行时依赖;数据访问的类型安全由生成代码保证
  - 前端仅构建期依赖 Vue 3 / TypeScript / Vite:运行时零 Node 依赖,产物 embed 进二进制
- 进程形态：单二进制 + SQLite 数据文件；参数 `--listen` / `--data-dir` / `--admin-password`（或环境变量），无配置文件
- 为后续变更预留的扩展点：协议适配器接口（C3 接跨协议 IR 转换）、中间件注册表（C2 / C4 挂载新环节）、`providers.protocol` 字段（C4 增加 `systemone`）
