# Spec Delta

## Purpose

定义网关对外提供的两类入向协议服务（OpenAI Chat Completions 与 Anthropic Messages）及其同协议上游直通行为，包括路由解析、流式保真、错误透传与凭据隔离。

## ADDED Requirements

### Requirement: OpenAI Chat Completions 入向

系统 **必须(MUST)** 提供 OpenAI Chat Completions 兼容端点 `POST /v1/chat/completions`,支持流式(SSE)与非流式请求,并直通转发至所选 provider 对应的 OpenAI 兼容上游。

#### Scenario: 非流式请求直通

- **WHEN** 客户端以 OpenAI Chat 协议发起非流式请求且 `model` 命中模型目录
- **THEN** 请求被转发至模型目录条目映射的上游,上游响应原样返回给客户端

#### Scenario: 流式请求直通

- **WHEN** 客户端以 `stream: true` 发起请求
- **THEN** 上游 SSE 数据逐块转发，客户端在生成过程中持续收到增量

### Requirement: Anthropic Messages 入向

系统 **必须(MUST)** 提供 Anthropic Messages 兼容端点 `POST /v1/messages`(按路径匹配,含带查询串的请求)并直通转发至所选 provider 对应的 Anthropic 原生上游;**必须** 同时透传 `POST /v1/messages/count_tokens` 端点。

#### Scenario: Anthropic 请求直通

- **WHEN** 客户端以 Anthropic Messages 协议发起请求且 `model` 命中模型目录
- **THEN** `anthropic-*` 头与 body 扩展字段按开放列表原样转发至 Anthropic 原生上游,上游响应原样返回

#### Scenario: 令牌计数端点透传

- **WHEN** 客户端请求 `/v1/messages/count_tokens`
- **THEN** 请求被透传至上游并原样返回计数结果

### Requirement: 流式保真

对流式响应,系统 **必须(MUST)** 逐块转发、**不得** 缓冲完整响应后再送出;**必须** 原样转发上游的 keep-alive / ping 事件(包括 SSE ping 与注释行);**必须** 保持 `text/event-stream` 响应类型。

#### Scenario: 长静默期间流不中断

- **WHEN** 上游在较长一段时间内仅发送 ping 事件而不产生内容
- **THEN** ping 事件被逐字节转发，客户端不因静默而中止连接

#### Scenario: 大响应不整体缓冲

- **WHEN** 上游产生一个持续数十秒的流式响应
- **THEN** 客户端在响应开始后即逐步收到数据，而非等待全部完成后一次性收到

### Requirement: 错误透传

上游的错误响应(HTTP 状态码与响应体)**必须(MUST)** 原样转发给客户端;系统 **不得** 用自有错误信封包裹上游错误。

#### Scenario: 上游错误原样到达

- **WHEN** 上游以协议原生错误结构返回 400 错误
- **THEN** 客户端收到的状态码与错误体与上游一致

### Requirement: 模型目录路由与未命中错误

系统 **必须(MUST)** 按请求 `model` 字段解析路由:命中模型目录条目时,路由至该条目映射的 provider 与上游模型;未命中任何条目时,**必须** 返回明确的「模型未配置路由」错误(按入向协议的错误格式),**不得** 静默转发至默认上游。

#### Scenario: 命中模型目录条目

- **WHEN** 请求 `model` 等于模型目录中某条目的可见名
- **THEN** 请求被转发至该条目配置的 provider,并以该条目配置的上游模型名发起调用

#### Scenario: 未命中模型目录条目

- **WHEN** 请求 `model` 不属于模型目录中任何条目的可见名
- **THEN** 系统返回明确的未路由错误,且不发起任何上游调用

### Requirement: 凭据隔离

系统 **必须(MUST)** 使用 provider 配置中的上游凭据发起上游调用;客户端凭据 **不得** 出现在发往上游的请求中。

#### Scenario: 客户端 key 不出站

- **WHEN** 客户端携带网关签发的 key 发起请求
- **THEN** 上游请求携带的是 provider 配置的凭据，且不包含客户端 key
