# Proposal

## Why

全局配置面板只读写 `settings` 表。当前没有任何运行时逻辑消费该表的键值。面板却占一个导航位，每次使用都要先判断它是否有用。逐模块优化的第一步是去掉这个空位。

## What Changes

- 移除侧栏「全局配置」入口，导航从五项减为四项。
- 移除 `web/src/views/Settings.vue` 及其在 `App.vue` 的映射。
- 移除前端 `Setting` 类型与它的调用。
- **保留**后端 `/console/api/settings` 接口、`settings` 表与既有测试。数据不删，接口不删，后续要用时可从 HTTP 直接访问。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `admin-console`: 「配置管理能力」要求从界面 CRUD 清单中移除 settings,并明确 settings 不再由控制台界面管理。

## Impact

- 前端:`web/src/App.vue`、`web/src/api.ts`,移除 `web/src/views/Settings.vue` 文件。
- 后端：无改动，接口与表保留。
- 数据：不迁移，既有 `settings` 记录也不动。
