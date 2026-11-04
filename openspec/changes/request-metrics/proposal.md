# Proposal

## Why

轨迹侧已有模型与工具耗时，网关层面的请求情况却没有汇总入口。多少请求、多少失败、延迟长尾多长、缓存是否生效，都只能逐条翻记录。

## What Changes

- 可观测页新增「请求指标」岛。它显示窗口内的计数、错误率、p50 / p95、缓存命中率与 token。
- 窗口可选 1 小时 / 24 小时 / 7 天 / 全部，默认 24 小时。
- 新增接口 `GET /console/api/metrics?window=…`。
- 时间构成从独立色条降级为一行数字，附工具占比。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `admin-console`: 新增「请求指标岛」要求。时间构成改为行内数字，不再要求单独时间轴。

## Impact

- 后端：一条汇总查询与一条延迟升序查询，新增处理函数与测试。
- 前端：新组件、页面挂载、工具耗时列增加 p95。
- 存储：无迁移，复用 `requests` 表。
