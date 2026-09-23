# ds-context 构建入口。
# 约定:构建动作统一经此驱动,不直接调用 go install(见 AGENTS.md)。
SHELL     := bash
GO        ?= go
GOFMT     ?= gofmt
GOLANGCI  ?= golangci-lint
SQLC      ?= sqlc
ARGS      ?=
BINARY    := bin/ds-context

.PHONY: generate build run test lint clean

# generate: 以 sqlc 从迁移与查询 SQL 生成类型安全代码(生成产物入库,勿手改)
generate:
	@if [ ! -f sqlc.yaml ]; then echo "generate: sqlc.yaml 不存在"; exit 1; fi
	$(SQLC) generate

# build: 构建前端(如已初始化)后编译单二进制到 bin/
build:
	@if [ -f web/package.json ]; then \
		echo ">> 构建前端 (web/)"; \
		( cd web && pnpm install && pnpm build ); \
	else \
		echo ">> web/package.json 不存在,跳过前端构建"; \
	fi
	$(GO) build -trimpath -o $(BINARY) ./cmd/ds-context

# run: 本地运行,示例: make run ARGS="--listen 127.0.0.1:8080"
run:
	$(GO) run ./cmd/ds-context $(ARGS)

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
