# Tasks

## 1. 回归防护先行

- [x] 1.1 在 `pkg/trace/chain_main_test.go` 增加 `TestRegression_OneShotFirstStepIsNotMain`：首步为一次性链，其后两条链重复出现，断言主链是最早的持续性链。运行 `go test ./pkg/trace/ -run TestRegression_OneShotFirstStepIsNotMain`，确认修复前失败。
- [x] 1.2 增加「全部链均不足两个非重复步」用例：断言主链是含会话首步的链。
- [x] 1.3 增加重试用例 `TestRegression_OneShotRetryDoesNotBecomeMain`：一次性请求被重试后仍不夺取主链。
- [x] 1.4 在 `pkg/admin/main_chain_test.go` 增加会话级用例：首步为标题链时，`skeletonJSON` 的 `main.steps` 覆盖对话链，旁路编号从 1 起且连续。
- [x] 1.5 断言主链首步的 skeleton、detail 与 story 文案：不再自称「会话首条」或「会话首步」。
- [x] 1.6 增加三条旁路链的编号连续性用例。

## 2. 链归属实现

- [x] 2.1 修改 `trace.AnalyzeSession`：先扫描至少两个非重复步的链，取第一条为主链；全部不足时取链 0。重复步不计入非重复步。运行 `go test ./pkg/trace/`，全部通过。
- [x] 2.2 把上下文清零的排除条件从 `j > 0` 改为 `Kind != ChainMain`。
- [x] 2.3 更新 `AnalyzeSession`、`ChainMain`、`DeltaNew` 的注释，写明主链判据、重试排除与「只用门槛、不比较步数」的理由。

## 3. 管理面与文案适配

- [x] 3.1 修改 `pkg/admin/traces.go`：旁路链编号改为遍历全部链并跳过主链，保持 1 起连续。运行 `go test ./pkg/admin/`，全部通过。
- [x] 3.2 核对 `stepNote`、`stepDetailJSON`、`kindStoryLabel` 的链首文案：主链首步与旁路链首步各自使用正确说法。
- [x] 3.3 核对 `chainSummary`：主链计数仍恒为 1，`side` 计数等于非主链数。
- [x] 3.4 更新 `web/src/trailLegend.ts` 与 `docs/trail-view.html` 的 `CHAIN_NOTE`，两处逐字一致。

## 4. 全量验证

- [x] 4.1 运行 `make test`，全部包通过。
- [x] 4.2 运行 `make lint`，0 issue，sqlc 无漂移。
- [x] 4.3 用会话 `1:141983eb-874a-4072-abbf-7bfd343e6efb` 的数据库副本复核：主路落在主对话链（复核时 84 步），旁路 3 条（标题、监控与子 agent），旁路 52 步。会话继续增长时步数随库变化，不锁死。
- [x] 4.4 运行 `pnpm typecheck` 与 `pnpm build`，确认前端产物包含新文案。
- [x] 4.5 运行 `make smoke`，全部通过。
