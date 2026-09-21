# ds-context

## 项目概要

- **语言**：Go 1.25+
- **规范流程**：OpenSpec(spec-driven),需求与行为规范统一由 `openspec/` 管理
- **构建入口**：Makefile(`build` / `test` / `lint` / `run` / `clean`,随首批代码落地)

```
openspec/       规范中枢(specs / changes / config.yaml)
cmd/            入口(随代码补充)
pkg/            业务包(随代码补充)
```

## 规范流程：OpenSpec

本项目采用 OpenSpec 做 spec-driven 开发：**一切改变对外行为的变更,先提案、后实现、再归档**。

### 目录结构

```
openspec/
  specs/          已定稿的系统行为规范(唯一事实源,按能力分目录)
  changes/        进行中的变更提案
    archive/      已归档变更
  config.yaml     项目上下文(context)与工件规则(rules)
```

### 变更生命周期

`explore(可选) → propose → 评审 → apply → archive`

| 阶段 | 技能 | 说明 |
|------|------|------|
| 厘清需求 | `openspec-explore` | 讨论与调研,不产出代码 |
| 提议 | `openspec-propose`(变更名或需求描述) | 生成提案:`proposal.md`(为什么/是什么)、`specs/<capability>/spec.md`(行为增量 delta)、`design.md`(怎么做)、`tasks.md`(实现清单) |
| 修订 | `openspec-update-change` | 修订既有提案,保持工件相互一致 |
| 实现 | `openspec-apply-change` | 按 `tasks.md` 实施,以已批准的 delta 为准 |
| 归档 | `openspec-archive-change` | 变更完成后归档,delta 合并回 `openspec/specs/` |
| 同步 | `openspec-sync-specs` | 只把 delta 同步到主 specs,不归档 |

底层 CLI 为 `openspec`(`list` / `status` / `show` / `validate` / `new change` 等)。本仓库已安装上述 skills(`.claude/skills/`),流程操作优先通过它们驱动。

### 硬性规则

1. **未提案不实现** —— 改变对外行为的变更,实现前必须存在对应 change,且 apply 所需工件(proposal / specs / tasks)齐备
2. **规划边界** —— propose 阶段只产出规划工件,不写业务代码;实现必须显式进入 apply
3. **specs 是唯一事实源** —— 实现必须遵循 `openspec/specs/` 记载的行为;需求变化先改规范(走提案)再改代码,禁止实现与规范静默偏离
4. **不手搭骨架** —— change 目录统一由 `openspec new change` 生成,不手写、不手改 `archive/`
5. **维护上下文** —— 项目定位 / 技术栈 / 约定变化时,同步更新 `openspec/config.yaml` 的 `context` 字段

### 免走流程的变更

不改变 `openspec/specs/` 已记录行为的改动可直接进行：拼写 / 注释修正、纯内部重构、构建与 CI 配置、依赖升级。

- Bug 修复：只是让实现回到 specs 记载的行为 → 直接修复 + 补回归测试;需要改变 specs 记载的行为 → 先走提案

## 编码规范

### 通用

- **依赖最小化**:新增第三方依赖需说明理由(维护状态、许可证);能用标准库解决的不引入依赖
- **命名**：清晰自文档化,避免无谓缩写;导出符号必须有 doc 注释
- **注释**：使用中文(与团队语言一致);注释解释“为什么”,不复述代码

### Go 约定

- 格式化：`gofmt`;import 分组用 `goimports`;提交前必须格式化
- 静态检查：`golangci-lint run`,基线启用 `gosec` / `errcheck` / `errname` / `govet` / `staticcheck` / `ineffassign` / `unused` / `unconvert` / `copyloopvar` / `noctx`
- 错误处理：
  - 不允许静默忽略 error;`_ =` 仅限有意为之并注明理由(测试代码豁免)
  - wrap 使用 `fmt.Errorf("...: %w", err)`,保留错误链
  - 错误值命名 `ErrXxx`,错误类型命名 `XxxError`

## 测试

- **TDD**：Red → Green → Refactor;测试覆盖率 ≥97%(排除 OS / 文件系统不可模拟路径)
- 测试命名 `TestXxx`,与实现同包;优先表驱动测试
- **Bug 修复必须附回归防护**：
  - 可测：编写 `TestRegression_<简述>`,断言命中根因
  - 不可测：修复点上方加 `// REGRESSION: <根因>。无法单测:<理由>`
  - 同一代码区域累积 ≥3 条 → 视为脆弱模块,优先重构而非继续修补

## 代码审查

- 完成较大的代码改动(涉及 3+ 文件或 50+ 行变更)后,**自动**启动代码审查,审查维度包括:逻辑正确性、边界条件、安全风险
- 审查完成后将结果直接反馈给用户,无需用户主动要求

## 构建与测试命令

统一使用 Makefile 目标(随首批代码落地),**禁止直接调用 `go install`**：

| 操作 | 命令 | | 测试范围 | 命令 |
|------|------|---|----------|------|
| 编译 | `make build` | | 单包 / 单文件 | `go test ./pkg/<name>/ -run TestXxx` |
| 运行 | `make run` | | 多包 / 跨包 | `make test` |
| 静态检查 | `make lint` | | 集成测试 | `make test-integration` |
| 清理 | `make clean` | | | |

Makefile 建立前,过渡使用 `go build ./...` / `go test ./...` / `golangci-lint run`。

## 提交策略

**禁止自动提交**。必须等待用户明确给出指令(如“提交”、“commit”)后方可执行 `git add` / `git commit` / `git push`。

## Commit 规范

[Conventional Commits](https://www.conventionalcommits.org/zh-hans/v1.0.0/) v1.0.0：

```
<type>(<scope>): <subject>
```

- `type`: `feat` / `fix` / `refactor` / `test` / `docs` / `chore`
- `scope`: 包名,多 scope 用 `/` 分隔
- `subject`: 中文祈使句,≤72 字符,不以句号结尾

```
feat(<pkg>): 新增 xxx 能力
fix(<pkg>): 修复 xxx 导致 yyy 的问题
```

## 文档规范

- 架构 / 流程 / 数据模型绘图优先使用 Mermaid
