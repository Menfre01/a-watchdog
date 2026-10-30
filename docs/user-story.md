# 全流程用户故事：从控制台配置到一次请求

> 故事对象：自托管 ds-context 网关的个人 / 小团队（本故事对应 C1「gateway-core」范围）。
> 主角小明的两类调用方：**Claude Code**（Anthropic Messages 协议）与**自写的 OpenAI SDK 脚本**（OpenAI Chat 协议）。
> 本文按「控制台配 Provider → Claude Code 接入 → 网关处理一次请求」的完整路径展开。

## 0. 小明要解决的问题

- 三家上游的 key 散落在各台机器的环境变量里,换台机器就要重新配一遍;
- 想把 Claude Code 换个更便宜的上游,得挨个改机器上的环境变量;
- 想知道「谁在什么时候调用了什么模型、花了多少 token」,却没有一个统一的地方看。

目标:一个**单二进制网关** + 一个**控制台**,把凭证、路由、记录收口。

## 1. 启动网关

```bash
./bin/ds-context \
  --listen 127.0.0.1:8399 \
  --data-dir ~/.ds-context \
  --admin-password '******'
```

- 单二进制、单实例;配置与请求记录都存在 `--data-dir` 下的 SQLite 文件里,重启不丢;
- 默认只监听本机(示例端口 8399);
- 浏览器打开 `http://127.0.0.1:8399/console` 进入控制台。

## 2. 控制台:配置多个 Provider

### 2.1 登录

用管理员口令登录(会话保持)。控制台自身不暴露配置数据给未登录请求。

### 2.2 新建 Provider(三个例子)

Provider 是「**接入**」实体:协议类型、上游地址、凭据、启用状态。三个例子:

| 字段 | anthropic-official | anthropic-relay | deepseek |
|---|---|---|---|
| 协议 | `anthropic-messages` | `anthropic-messages` | `openai-chat` |
| Base URL | `https://api.anthropic.com` | `https://relay.example.com` | `https://api.deepseek.com` |
| API Key | `sk-ant-…` | `sk-relay-…` | `sk-…` |
| 测试模型 | `claude-haiku-4-5` | `claude-haiku-4-5` | `deepseek-chat` |
| 用途 | Claude Code 主线路 | Claude Code 备用/更便宜线路 | 脚本调用 |

每个都点一次「**测试连接**」——网关以「测试模型」向上游发起一条 1-token 的最小对话请求,一次确认地址、凭据与模型可用。

### 2.3 建立模型目录

模型目录是「**配置**」实体:**客户端可见名 → Provider + 上游模型名**。它只引用 Provider,不冗余地址与凭据——以后换地址/换 key 只动 Provider,目录不动。

| 客户端可见名 | Provider | 上游模型名 |
|---|---|---|
| `claude-sonnet-4-5` | anthropic-official | `claude-sonnet-4-5` |
| `claude-opus-4-8` | anthropic-official | `claude-opus-4-8` |
| `claude-sonnet-4-5-relay` | anthropic-relay | `claude-sonnet-4-5` |
| `deepseek-chat` | deepseek | `deepseek-chat` |

### 2.4 生成客户端 Key

- 生成两个:**`claude-code-小明`**、**`scripts`**;
- 明文只在生成时展示一次,库里只存 SHA-256 哈希;
- 随时可停用——停用即时生效,无需重启。

## 3. 在 Claude Code 中配置

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8399
export ANTHROPIC_AUTH_TOKEN=dsctx_xxxxxxxx    # 网关生成的 client key
claude
```

- 网关同时接受 `Authorization: Bearer <key>`(对应 `ANTHROPIC_AUTH_TOKEN`)与 `x-api-key: <key>`(对应 `ANTHROPIC_API_KEY`),两种配法都行;
- Claude Code 里使用的模型名(如 `claude-sonnet-4-5`)必须在**模型目录**里存在;不在目录里的模型会收到明确的 404 错误,而不是被悄悄转发到某家上游;
- 同类:OpenAI SDK 把 `base_url` 指向 `http://127.0.0.1:8399/v1`、`api_key` 填网关签发的 key 即可。

## 4. 网关如何处理一次请求

场景:小明在 Claude Code 里发出了一条消息。

```mermaid
sequenceDiagram
    participant CC as Claude Code
    participant GW as ds-context 网关
    participant UP as 上游 Provider(anthropic-official)

    CC->>GW: POST /v1/messages?beta=true(流式,model=claude-sonnet-4-5)
    Note over GW: ① 鉴权:client key → 内存快照校验
    Note over GW: ② 路由:模型目录 → provider + 上游模型名
    GW->>UP: POST /v1/messages(替换为上游凭据;anthropic-* 头原样转发)
    UP-->>GW: SSE 流(含 ping)
    GW-->>CC: 逐块转发,不缓冲
    Note over GW: ④ 记录:异步写入请求记录(含被拒请求)
```

按固定阶段序拆开看:

### ① 准入(鉴权)

网关从 `Authorization` / `x-api-key` 取出 client key,在内存快照里校验:有效且启用 → 放行;无效、停用或缺失 → 立即返回 401(响应体是 Anthropic 协议风格的错误结构),请求到此短路,不再进入后续阶段。

### ② 路由 / 适配

按请求里的 `model=claude-sonnet-4-5` 查模型目录:

- 命中 → 得到 `provider=anthropic-official`、上游模型名 `claude-sonnet-4-5`;
- 未命中 → 返回 404(`not_found_error`)并**不发起任何上游调用**——绝不静默转发。

### ③ 调用上游

- **凭据替换**:以上游 Provider 配置的凭据发起调用,小明的 client key 绝不流出网关;
- **开放列表转发**:`anthropic-version` / `anthropic-beta` 等 `anthropic-*` 头与 body 扩展字段原样转发;
- **流式保真**:响应逐块转发、不缓冲;上游的 keep-alive / ping(包括 SSE 注释行)也原样转发——Claude Code 在静默 300 秒后会主动断流,ping 是长思考期间唯一的流量;
- **错误透传**:上游的错误响应原样到达客户端,网关不加自己的错误信封(客户端依赖错误原文做恢复逻辑)。

### ④ 响应后处理(记录)

一条请求记录异步落库:时间、client key、入向协议、请求模型、实际 provider / 上游模型、状态码、延迟、usage、流式标记。**被拒绝的请求也会被记录**;记录永不阻塞请求——写库失败只影响日志,不影响小明的响应。

## 5. 换个上游:改一行,客户端无感

小明想把 Claude Code 切到更便宜的中转:

1. 控制台里把 `claude-sonnet-4-5` 条目的 Provider 从 `anthropic-official` 改成 `anthropic-relay`;
2. 保存即生效——之后到达的请求立刻走新上游;
3. Claude Code 侧什么都不用改。想切回来,再改回去。

这就是「配置与接入分离」的日常价值:**客户端只认稳定的可见名,上游怎么接、接去哪,全在控制台里拧**。

## 附注:范围与后续

- 本文对应 **C1(gateway-core)** 已定稿的行为范围;
- 跨协议转换(如让 Claude Code 直接指向 OpenAI 兼容的 DeepSeek)由 **C3** 提供;本故事中 Anthropic 入向走 Anthropic 协议上游;
- 上下文轨迹(session 判定、git 快照、压缩分析)随 **C2** 落地、jev 智能选型随 **C4** 落地,之后在本文件补充对应章节;
- 走完全文即 C1 的验收闭环:**控制台配好上游与 key → Claude Code 流式跑通 → 网关能看到请求记录**。
