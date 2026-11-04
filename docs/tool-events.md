# 工具事件上报

客户端在工具执行前后各上报一次，网关把同一 `tool_use_id` 的两次事件配成一条调用记录。这是目前唯一能拿到工具耗时的低侵入方式。

## 时长口径

**不是纯执行时间**。记录的是「开始事件到达网关」到「结束事件到达网关」的墙钟差，包含:

- 工具真正执行。
- hook 分发与进程启动(毫秒级)。
- HTTP 回环(亚毫秒)。
- 权限审批等待(如果有，可达数秒到数分钟)。

记录里带 `boundary=hook` 与 `approval_wait` 标记。没有结束事件的调用显示为未完成，不推算时长。

## 端点

```
POST /v1/tool-events?client=claude-code|codex|dsh
Authorization: Bearer <客户端密钥>
```

幂等键是 `(client, tool_call_id, phase)`:重复上报只保留第一条。

## Claude Code

HTTP hook 直接把事件 JSON 发到网关，不需要脚本:

```json
{
  "hooks": {
    "PreToolUse": [{ "matcher": "*", "hooks": [{
      "type": "http",
      "url": "http://127.0.0.1:8399/v1/tool-events?client=claude-code",
      "timeout": 5,
      "headers": { "Authorization": "Bearer $DSCTX_KEY" },
      "allowedEnvVars": ["DSCTX_KEY"]
    }]}],
    "PostToolUse": [{ "matcher": "*", "hooks": [{
      "type": "http",
      "url": "http://127.0.0.1:8399/v1/tool-events?client=claude-code",
      "timeout": 5,
      "headers": { "Authorization": "Bearer $DSCTX_KEY" },
      "allowedEnvVars": ["DSCTX_KEY"]
    }]}],
    "PostToolUseFailure": [{ "matcher": "*", "hooks": [{
      "type": "http",
      "url": "http://127.0.0.1:8399/v1/tool-events?client=claude-code",
      "timeout": 5,
      "headers": { "Authorization": "Bearer $DSCTX_KEY" },
      "allowedEnvVars": ["DSCTX_KEY"]
    }]}]
  }
}
```

把 `DSCTX_KEY` 设成控制台里生成的密钥。hook 失败默认不阻塞工具调用。上报失败不会影响 Claude Code 正常工作。

## Codex

Codex 只支持 command hook,需要一个小脚本把载荷转成同样的字段:

```toml
# ~/.codex/config.toml
[[hooks.PreToolUse]]
matcher = ""

[[hooks.PreToolUse.hooks]]
type = "command"
command = "/path/to/dsctx-tool-event.sh pre"
timeout = 5

[[hooks.PostToolUse]]
matcher = ""

[[hooks.PostToolUse.hooks]]
type = "command"
command = "/path/to/dsctx-tool-event.sh post"
timeout = 5
```

脚本从 stdin 读事件 JSON,补上阶段后转发。

```bash
#!/usr/bin/env bash
phase="$1"
jq --arg phase "$phase" '. + {phase: $phase}' \
  | curl -sS -X POST "http://127.0.0.1:8399/v1/tool-events?client=codex" \
      -H "Authorization: Bearer ${DSCTX_KEY}" \
      -H 'Content-Type: application/json' \
      --data-binary @- >/dev/null
```

Codex 没有失败钩子。失败的工具调用可能只有开始事件，读取时显示为未完成。

## DSH

DSH 内部已有 `tool/call` 与 `tool/result` 事件。写一个插件订阅两者，分别以 `phase=start` 与 `phase=end` 调用同一端点，载荷用规范字段:

```json
{
  "session_id": "<会话标识>",
  "turn_id": "<轮次标识>",
  "tool_call_id": "<工具调用标识>",
  "tool_name": "Bash",
  "phase": "start"
}
```

进程内取时间，比外部 hook 少一层进程开销。

## 验证

```bash
curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST 'http://127.0.0.1:8399/v1/tool-events?client=claude-code' \
  -H 'Authorization: Bearer <密钥>' -H 'Content-Type: application/json' \
  -d '{"hook_event_name":"PreToolUse","session_id":"s1","tool_use_id":"t1","tool_name":"Bash"}'
```

返回 204 即落库成功。同一请求再发一次不会新增行。
