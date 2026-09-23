package config

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrate 依据 PRAGMA user_version 依次应用内嵌迁移。
// 迁移文件名形如 NNNN_name.sql,前缀即版本号;已应用的版本跳过,重复启动幂等。
func migrate(ctx context.Context, sqlDB *sql.DB) error {
	var current int
	if err := sqlDB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("config: read user_version: %w", err)
	}
	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("config: glob migrations: %w", err)
	}
	sort.Strings(names)
	for i, name := range names {
		want := i + 1
		base := strings.TrimSuffix(path.Base(name), ".sql")
		prefix, _, ok := strings.Cut(base, "_")
		if !ok {
			return fmt.Errorf("config: migration %q: 文件名需形如 NNNN_name.sql", name)
		}
		n, err := strconv.Atoi(prefix)
		if err != nil || n != want {
			return fmt.Errorf("config: migration %q: 序号应为 %04d", name, want)
		}
		if n <= current {
			continue
		}
		ddl, err := migrationsFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("config: read %s: %w", name, err)
		}
		if err := applyMigration(ctx, sqlDB, n, string(ddl)); err != nil {
			return fmt.Errorf("config: apply %s: %w", name, err)
		}
	}
	return nil
}

// applyMigration 在单个事务里执行一个迁移并推进 user_version。
func applyMigration(ctx context.Context, sqlDB *sql.DB, version int, ddl string) error {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// 提交成功后 Rollback 为无操作,此处显式忽略其错误。
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("exec ddl: %w", err)
	}
	// #nosec G201 -- 拼入的是代码内固定的迁移序号(整数),无注入面。
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
