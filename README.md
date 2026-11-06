# AWatchDog

面向个人与小团队的自托管 LLM 网关与轨迹分析控制台。

AWatchDog 位于 Claude Code、Codex、DeepSeek Harness（DSH）或 OpenAI SDK 与上游模型之间。它统一管理上游凭证、模型路由与客户端密钥，并记录每次请求和会话轨迹。所有数据保存在本地 SQLite，单二进制运行。

## 功能

- 支持 Anthropic Messages 与 OpenAI Chat Completions 两种入向协议。
- 同协议直通上游，流式响应逐块转发。
- 控制台管理接入、模型目录与客户端密钥。
- 记录请求、响应与会话轨迹，按轮次和 step 查看。
- 查看工具调用、耗时、状态与发起 step 的请求上下文。
- 汇总请求数、错误率、p50/p95 延迟、缓存命中与 token 用量。
- 接收客户端工具事件与提示事件。

## 快速开始

需要 Go 1.25+、pnpm 与 Node.js。

克隆仓库后，在仓库根目录构建：

```bash
make build
```

首次构建会安装前端依赖并编译单二进制。

启动时优先用环境变量传管理员口令，避免口令进入 shell 历史：

```bash
export AWATCHDOG_ADMIN_PASSWORD='替换为你的口令'
./bin/awatchdog --listen 127.0.0.1:8399 --data-dir ./.awatchdog
```

SQLite 数据保存在 `--data-dir`，默认是 `~/.awatchdog`。

打开 <http://127.0.0.1:8399/console>，用管理员口令登录。

也可以用 `--admin-password` 传口令。未配置口令时，控制台登录不可用。

## 配置

在控制台完成三步：

1. **接入**：创建 Provider，填写协议、Base URL、API Key 与测试模型。
2. **模型目录**：创建客户端可见名称到 Provider 和上游模型的映射。
3. **客户端密钥**：创建网关 key。明文只显示一次。

### Claude Code

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8399
export ANTHROPIC_AUTH_TOKEN=awd_xxxxxxxx
claude
```

### OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8399/v1",
    api_key="awd_xxxxxxxx",
)
```

替换 model、key 与上游配置后，即可从控制台观察请求和会话。

## 观测

控制台「可观测」模块是只读视图：

- **会话**：按密钥、时间与标识前缀筛选。
- **轨迹**：按轮次与 step 展开请求、响应与工具调用。
- **工具**：查看工具调用明细、状态、耗时与上下文。
- **指标**：请求数、错误率、延迟分位、缓存命中与 token 用量。

工具事件的接入方式见 [docs/tool-events.md](docs/tool-events.md)。

## 数据与安全

- 默认仅监听 `127.0.0.1`。
- 请求、响应、工具入参与结果可能包含敏感信息。`--data-dir` 应放在受控目录。
- 控制台管理员口令与客户端 key 分开管理。
- 如需暴露到局域网或公网，请在前面加反向代理、TLS 与访问控制。
- SQLite 文件与日志不要提交到版本库。
