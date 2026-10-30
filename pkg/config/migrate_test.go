package config

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpen_CreatesSchemaAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// 重复启动幂等:再次打开不报错、版本不回退、数据保留。
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen Open() error = %v", err)
	}
	defer func() {
		if err := s2.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	var version int
	if err := s2.sqlDB.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version error = %v", err)
	}
	if version != 3 {
		t.Fatalf("user_version = %d, want 3(0001+0002+0003 个迁移)", version)
	}
	if providers, err := s2.ListProviders(context.Background()); err != nil || len(providers) != 0 {
		t.Fatalf("ListProviders() = %v, %v; want empty, nil", providers, err)
	}
}
