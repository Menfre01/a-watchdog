# Tasks

## 1. 代码:显示词与判定理由

- [x] 1.1 判定理由:分类 `chain` 的理由改为陈述「与同链上一步无共同前缀也无共享尾部」,不再称「新链」。验证:`pkg/trace` 的表驱动测试期望同步更新并通过
- [x] 1.2 前端显示词:分类表、徽标回退表、图例条目、分布条文案、轮级标记(改为「不相交」)同步改词。验证:`pnpm typecheck` 与 `make build` 通过
- [x] 1.3 口径文案:分布条与图例里提到该分类的文案改词。另补一句说明两种判据的分工:链归属用稳定指纹,内容分类用整行字节。验证:文案取自 `trailLegend.ts` 单一来源

## 2. 原型与冒烟

- [x] 2.1 `docs/trail-view.html` 同步全部用词与配色注释。验证:原型渲染结果不再出现「新链」
- [x] 2.2 冒烟脚本断言分布条与图例出现「无重合」,并断言渲染结果中**不含**「新链」。验证:`make smoke` 退出码为 0(共 31 项断言)

## 3. 规范用词同步(三个未归档变更)

- [x] 3.1 `trace-system`:`trajectory-analysis` 的分类名、`admin-console` 的徽标与判定理由措辞、`design.md` 的分类表与形态清单、`tasks.md` 与 `proposal.md` 的提及一并改词。验证:`openspec validate trace-system --strict` 通过
- [x] 3.2 `trail-view-readability` 与 `kinds-bar-caliber`:图例与分布条里提到该分类的条目改词。验证:两个变更各自 `--strict` 校验通过

## 4. 验收

- [x] 4.1 全量验证:`make test`、`golangci-lint run`、`gofmt -l`、`pnpm typecheck`、`make build`、`make smoke`、`openspec validate no-overlap-wording --strict`,并对涉及改词的四个变更各跑一次 `--strict`
