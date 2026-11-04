# Design

## Context

见 `proposal.md` 的 Why。工具事件已有端点与表，鉴权逻辑可复用。Claude Code 的 `UserPromptSubmit` 在提示提交时触发，载荷含 `session_id`、`prompt_id` 与 `prompt`。

## Goals / Non-Goals

**Goals:**

- 采集提交时刻，足以算出人的间隔。
- 不保存提示正文。
- 重复上报不产生重复行。

**Non-Goals:**

- 本轮不在控制台展示。
- 不推算人的间隔本身：它需要与上一步结束时间配对，留到统计卡改造时做。

## Decisions

### 1. 独立表，不复用 tool_events

提示提交没有工具标识与阶段，塞进 `tool_events` 需要引入空字段与伪工具名。单独一张 `prompt_events` 更清楚。

### 2. 只存长度

`prompt_len` 足够做统计，正文属于用户内容，不落库。

### 3. 有 id 才去重

`prompt_id` 存在时按 `(client, session_id, prompt_id)` 唯一。缺失时不去重，因为无法判断是否同一次提交。

### 4. 脚本按事件名分流

同一个转发脚本读 `hook_event_name`,`UserPromptSubmit` 走 `/v1/prompt-events`,其余走 `/v1/tool-events`。settings 里只多一条 hook,不新增脚本。

## Risks / Trade-offs

- **人的间隔含客户端处理** → 提交时刻比“人按下回车”略晚，误差在几十毫秒，可接受。
- **没有 prompt_id 时可能重复** → 接受重复，统计时按时间聚类更稳妥。
- **正文不存** → 无法回看用户说了什么。需要时只能从轨迹快照里看。

## Migration Plan

新增 0005 迁移，只建表。回滚为删除表、端点与一条 hook。
