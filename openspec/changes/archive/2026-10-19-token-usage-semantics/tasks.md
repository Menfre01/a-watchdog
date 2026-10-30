# Tasks

## 1. 后端:协议字段

- [x] 1.1 每步骨架新增 `protocol`,取自记录侧的 `requests.protocol`。验证:端到端测试断言步骨架里的协议与写入时一致
- [x] 1.2 口径写进代码:字段注释说明它决定用量列的口径,视图据此标注。验证:代码审查确认注释说明用途

## 2. 前端:协议标记与口径

- [x] 2.1 会话头给出该会话出现的协议(从各步去重得出)。单协议时只显示该协议。多协议时列出全部,并给出「跨协议用量不可相加」的说明。验证:`pnpm typecheck` 与 `make build` 通过
- [x] 2.2 图例新增**用量口径**分组,含 Anthropic 与 OpenAI 兼容两套口径(取自 `trailLegend.ts` 单一来源)。验证:图例条目与常量表一致
- [x] 2.3 步级别可查协议:步详情常驻显示协议,步行在多协议会话里另给协议标记,便于逐条判断。验证:混合协议的场景下每步协议可见

## 3. 原型与冒烟

- [x] 3.1 `docs/trail-view.html` 覆盖协议标记、用量口径分组与多协议说明。验证:原型渲染出协议与两套口径文案
- [x] 3.2 冒烟脚本断言协议标记与两条口径文案。验证:`make smoke` 退出码为 0(共 27 项断言)

## 4. 验收

- [x] 4.1 全量验证:`make test`、`golangci-lint run`、`gofmt -l`、`pnpm typecheck`、`make build`、`make smoke`、`openspec validate token-usage-semantics --strict` 全部通过
