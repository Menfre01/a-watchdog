package config

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
)

// tableColumns 返回指定表的列名集合(用 SQLite 的表值函数,表名走参数绑定)。
func tableColumns(t *testing.T, s *Store, table string) map[string]bool {
	t.Helper()
	rows, err := s.sqlDB.QueryContext(context.Background(), "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		t.Fatalf("查询 %s 列信息失败: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("扫描列名失败: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历列失败: %v", err)
	}
	return got
}

// hasIndex 判断具名索引是否存在。
func hasIndex(t *testing.T, s *Store, name string) bool {
	t.Helper()
	var n int
	if err := s.sqlDB.QueryRowContext(context.Background(),
		"SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&n); err != nil {
		t.Fatalf("查询索引 %s 失败: %v", name, err)
	}
	return n > 0
}

// TestOpen_CreatesTraceTables 断言 0003 迁移落地的两张表与索引齐备,且版本推进到 3。
func TestOpen_CreatesTraceTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	var version int
	if err := s.sqlDB.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version error = %v", err)
	}
	if version != 3 {
		t.Fatalf("user_version = %d, want 3(0001+0002+0003)", version)
	}

	// 逐列断言:迁移漏列时直接定位到具体列。
	cases := []struct {
		table string
		cols  []string
	}{
		{"request_bodies", []string{"request_id", "method", "path", "query", "body_gzip", "headers", "created_at"}},
		{"snapshots", []string{
			"id", "request_id", "session_id", "session_source", "session_note",
			"seq", "ts", "ndjson_gzip", "line_hashes", "line_sizes",
		}},
	}
	for _, c := range cases {
		got := tableColumns(t, s, c.table)
		if len(got) == 0 {
			t.Errorf("表 %s 不存在或没有列", c.table)
			continue
		}
		for _, want := range c.cols {
			if !got[want] {
				names := make([]string, 0, len(got))
				for n := range got {
					names = append(names, n)
				}
				sort.Strings(names)
				t.Errorf("表 %s 缺少列 %q(现有: %v)", c.table, want, names)
			}
		}
	}

	if !hasIndex(t, s, "idx_snapshots_session") {
		t.Error("缺少索引 idx_snapshots_session")
	}
}
