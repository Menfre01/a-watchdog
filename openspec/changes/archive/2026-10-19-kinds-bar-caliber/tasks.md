# Tasks

## 1. 后端:锁定不变量

- [x] 1.1 测试锁定:非空快照下,分类为「首步」的步数等于链总数(主链加旁路链)。验证:构造含旁路链的会话,断言 `kinds` 里首步数等于链数。该断言失败即表示分类或归属有问题

## 2. 前端:两处口径

- [x] 2.1 分布条旁给出「无重合」的口径与「首步 = 链数」的不变量(含链首空快照的退化例外)。验证:`pnpm typecheck` 与 `make build` 通过
- [x] 2.2 图例中该分类的条目补同一句口径,文案取自 `trailLegend.ts` 单一来源。验证:分布条与图例的说法一致

## 3. 原型与冒烟

- [x] 3.1 `docs/trail-view.html` 覆盖这两处说明。验证:原型渲染出该分类的口径与不变量文案
- [x] 3.2 冒烟脚本断言两处文案,并把全部口径文案纳入「原型与构建产物」比对。验证:`make smoke` 退出码为 0(共 29 项断言)

## 4. 验收

- [x] 4.1 全量验证:`make test`、`golangci-lint run`、`gofmt -l`、`pnpm typecheck`、`make build`、`make smoke`、`openspec validate kinds-bar-caliber --strict` 全部通过
