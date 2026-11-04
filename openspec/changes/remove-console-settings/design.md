# Design

## Context

见 `proposal.md` 的 Why。控制台的 settings 面板对应后端一组已存在的接口与测试。`settings` 表由 `pkg/config` 读取进快照,当前运行时没有消费任何键。

## Goals / Non-Goals

**Goals:**

- 侧栏只保留四个有实际用途的入口。
- 移除后不留下死代码与无用类型。

**Non-Goals:**

- 不删后端接口、数据库表或测试。
- 不改其他模块的界面与行为。

## Decisions

### 1. 只改前端,保留后端

接口与表保留。理由:接口有测试覆盖,数据可能已有记录。移除不可逆且没有收益。代价是 settings 暂时只能经 HTTP 读写,这与它当前无人消费的现状一致。

### 2. 顺带移除前端类型

`Setting` 类型只服务 `Settings.vue`。该视图移除后,类型即成死代码,一并移除。

### 3. 不做跳转兼容

旧地址没有路由,视图由 tab 状态映射,不存在可访问的旧链接,无需重定向。

## Risks / Trade-offs

- **以后要用 settings 得先加回界面** → 恢复那个视图与一行 tab 映射即可,代价可控。
- **有人依赖界面写 settings** → 当前运行时没有键消费,风险为零。接口仍可写。

## Migration Plan

无数据迁移。回滚方式为恢复 `Settings.vue` 与 `App.vue` 的映射。
