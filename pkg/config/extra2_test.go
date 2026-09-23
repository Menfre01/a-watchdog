package config

import (
	"context"
	"path/filepath"
	"testing"
)

// TestRefresh_ErrorBranches 逐表破坏后重建快照,覆盖各实体的读取失败分支。
func TestRefresh_ErrorBranches(t *testing.T) {
	tables := []string{"providers", "models", "client_keys", "settings"}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			// #nosec G201 -- 表名来自本测试内的白名单常量,无注入面。
			if _, err := s.sqlDB.ExecContext(ctx, "DROP TABLE "+table); err != nil {
				t.Fatalf("drop table: %v", err)
			}
			if err := s.Refresh(ctx); err == nil {
				t.Fatalf("Refresh() with missing %s error = nil", table)
			}
		})
	}
}

func TestApplyMigration_BeginError(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	ctx := context.Background()
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	// 已关闭的连接:开启事务失败(覆盖 applyMigration 的 Begin 错误分支)。
	if err := applyMigration(ctx, s.sqlDB, 99, "CREATE TABLE t (id INTEGER)"); err == nil {
		t.Fatalf("applyMigration(closed db) error = nil")
	}
}
