# awatchdog 构建入口。
# 约定:构建动作统一经此驱动,不直接调用 go install(见 AGENTS.md)。
SHELL     := bash
GO        ?= go
GOFMT     ?= gofmt
GOLANGCI  ?= golangci-lint
SQLC      ?= sqlc
ARGS      ?=
STE_FILES ?=
STE_BASELINE ?= 0
BINARY    := bin/awatchdog

.PHONY: generate build run test lint lint-text smoke clean coverage

# generate: 以 sqlc 从迁移与查询 SQL 生成类型安全代码(生成产物入库,勿手改)
generate:
	@if [ ! -f sqlc.yaml ]; then echo "generate: sqlc.yaml 不存在"; exit 1; fi
	$(SQLC) generate

# build: 构建前端(如已初始化)后编译单二进制到 bin/
# 前端产物文件名带内容哈希,而 vite 不清空 outDir(目录内有 git 跟踪的占位文件),
# 故构建前显式清理 assets:否则历次产物持续堆积并被 go:embed 打进二进制。
build:
	@if [ -f web/package.json ]; then \
		echo ">> 构建前端 (web/)"; \
		rm -rf pkg/admin/dist/assets; \
		( cd web && pnpm install && pnpm build ); \
	else \
		echo ">> web/package.json 不存在,跳过前端构建"; \
	fi
	$(GO) build -trimpath -o $(BINARY) ./cmd/awatchdog

# run: 本地运行,示例: make run ARGS="--listen 127.0.0.1:8080"
# 口令默认 admin(仅本机开发);改口令用 make run ADMIN_PASSWORD=xxx。
# 数据目录默认在仓库内,避免沙箱或只读工作区把 SQLite 变成只读。
ADMIN_PASSWORD ?= admin
DATA_DIR ?= ./.awatchdog
run:
	@mkdir -p $(DATA_DIR)
	$(GO) run ./cmd/awatchdog --data-dir $(DATA_DIR) --admin-password $(ADMIN_PASSWORD) $(ARGS)

# coverage: 按包输出覆盖率与 COVERAGE-EXEMPT 标记数量
coverage:
	@bash scripts/coverage.sh

# smoke:轨迹视图原型冒烟 —— 断言形态基准渲染出新形态,并与前端图例文案比对
smoke:
	@command -v node >/dev/null || { echo "smoke: 未找到 node"; exit 1; }
	node scripts/trail-view-smoke.mjs

# test: 运行全部测试
test:
	$(GO) test ./...

# lint: gofmt 检查 + golangci-lint 基线(配置见 .golangci.yml)
lint:
	@files="$$($(GOFMT) -l cmd pkg)"; \
	if [ -n "$$files" ]; then \
		echo "gofmt 检查失败,以下文件需要格式化:"; \
		echo "$$files"; \
		exit 1; \
	fi
	$(GOLANGCI) run
	$(SQLC) diff

# clean: 清理构建产物
clean:
	rm -rf bin

# lint-text: 文字规范机械首查(默认扫 openspec 与 docs,STE_FILES 限定文件,STE_BASELINE 容忍存量)
# 脚本源自 lemonhall/asd-ste100-skill-zh(MIT,许可文本见 scripts/ste-lint-zh.LICENSE.txt)
lint-text:
	@command -v python3 >/dev/null || { echo "lint-text: 未找到 python3"; exit 1; }
	@files="$(STE_FILES)"; \
	if [ -z "$$files" ]; then files="$$(find openspec docs -name '*.md' -type f 2>/dev/null | sort)"; fi; \
	if [ -z "$$files" ]; then echo "lint-text: 没有待检查的文档"; exit 0; fi; \
	python3 scripts/ste-lint-zh.py --baseline $(STE_BASELINE) $$files
