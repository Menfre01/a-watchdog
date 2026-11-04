# Proposal

## Why

可观测的会话列表与详情靠组件内状态切换。点「查看」后列表卸载，返回时滚动位置丢失。刷新与浏览器后退都回不到某个会话。读者在列表与轨迹之间来回比对时，每次都要重新找到原来的位置。

## What Changes

- 地址栏表达导航状态:`#/observability/<session_id>?panel=trace|tools`。刷新与后退可回到同一个会话与面板，链接可分享。
- 列表在进入详情后 **保活**（`v-show`），返回时恢复滚动位置并把选中行滚回视野。
- 删除「查看」按钮。会话标识单元格改为可聚焦的链接，整行点击作为附加手段保留。
- 焦点管理：进入详情时焦点落在详情标题，返回时落回触发的那一行。
- 详情头部补上 steps 与 in/out/cache,顺序与列表行一致，减少视线跳跃。
- 轨迹面板仍挂载即卸，不做 KeepAlive:单会话可能数百个节点。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `admin-console`: 新增「可观测导航状态」要求（深链、保活、焦点回收）。「会话列表视图」的行入口由按钮改为链接。

## Impact

- 前端：新增 `web/src/route.ts`(hash 解析与订阅)、`Observability.vue` 的导航与保活、`TracePanel.vue` 的焦点锚点。
- 后端与存储：无改动。
- 依赖：不引入路由库。
