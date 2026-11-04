package config

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"awatchdog/pkg/config/db"
)

// TestCreate_RefreshErrorBranches 覆盖三个 Create 在快照刷新失败时的错误分支。
func TestCreate_RefreshErrorBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateProvider", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.sqlDB.ExecContext(ctx, "DROP TABLE settings"); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		params := db.CreateProviderParams{Name: "p", Protocol: "openai-chat", BaseUrl: "x", ApiKey: "k", Enabled: 1}
		if _, err := s.CreateProvider(ctx, params); err == nil {
			t.Fatalf("CreateProvider() with broken refresh error = nil")
		}
	})
	t.Run("CreateModel", func(t *testing.T) {
		s := newTestStore(t)
		p := mustProvider(t, s, "p")
		if _, err := s.sqlDB.ExecContext(ctx, "DROP TABLE settings"); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		params := db.CreateModelParams{Name: "m", ProviderID: p.ID, UpstreamModel: "u", Enabled: 1}
		if _, err := s.CreateModel(ctx, params); err == nil {
			t.Fatalf("CreateModel() with broken refresh error = nil")
		}
	})
	t.Run("CreateClientKey", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.sqlDB.ExecContext(ctx, "DROP TABLE settings"); err != nil {
			t.Fatalf("drop table: %v", err)
		}
		params := db.CreateClientKeyParams{Name: "k", KeyHash: "h", KeyPrefix: "p", Enabled: 1}
		if _, err := s.CreateClientKey(ctx, params); err == nil {
			t.Fatalf("CreateClientKey() with broken refresh error = nil")
		}
	})
}

// TestOpen_RefreshFailsOnVersionedEmptyDB 覆盖 Open 中初始快照构建失败的分支:
// 数据库声称版本已是最新(user_version=1)但表不存在。
func TestOpen_RefreshFailsOnVersionedEmptyDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.ExecContext(context.Background(), "PRAGMA user_version = 1"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	if _, err := Open(path); err == nil {
		t.Fatalf("Open(versioned empty db) error = nil, want refresh failure")
	}
}
