package config

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"ds-context/pkg/config/db"
)

func TestHashClientKey_StableAndDistinct(t *testing.T) {
	if got := HashClientKey("abc"); got != HashClientKey("abc") || len(got) != 64 {
		t.Fatalf("hash not stable/64-hex: %q", got)
	}
	if HashClientKey("abc") == HashClientKey("abd") {
		t.Fatalf("different keys hash identically")
	}
}

func TestBoolToInt(t *testing.T) {
	if BoolToInt(true) != 1 || BoolToInt(false) != 0 {
		t.Fatalf("BoolToInt mismatch: %d %d", BoolToInt(true), BoolToInt(false))
	}
}

func TestOpen_InvalidPathFails(t *testing.T) {
	// 以目录作为数据文件路径:sqlite 无法打开,Open 必须显式报错。
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatalf("Open(dir) error = nil, want failure")
	}
}

func TestStore_NotFoundAndEmptyLists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.GetProvider(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetProvider(missing) error = %v, want ErrNotFound", err)
	}
	p := mustProvider(t, s, "p1")
	if got, err := s.GetProvider(ctx, p.ID); err != nil || got.Name != "p1" {
		t.Fatalf("GetProvider() = %+v, %v", got, err)
	}

	if keys, err := s.ListClientKeys(ctx); err != nil || len(keys) != 0 {
		t.Fatalf("ListClientKeys() = %v, %v", keys, err)
	}
	if settings, err := s.ListSettings(ctx); err != nil || len(settings) != 0 {
		t.Fatalf("ListSettings() = %v, %v", settings, err)
	}
	if n, err := s.CountRequests(ctx); err != nil || n != 0 {
		t.Fatalf("CountRequests() = %d, %v", n, err)
	}
	if rows, err := s.ListRecentRequests(ctx, 10); err != nil || len(rows) != 0 {
		t.Fatalf("ListRecentRequests() = %v, %v", rows, err)
	}
}

// TestStore_ClosedErrors 覆盖各方法在底层连接不可用时的错误分支。
func TestStore_ClosedErrors(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	ctx := context.Background()
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err1 := s.ListProviders(ctx)
	_, err2 := s.CreateProvider(ctx, db.CreateProviderParams{})
	_, err3 := s.GetProvider(ctx, 1)
	err4 := s.UpdateProvider(ctx, db.UpdateProviderParams{})
	err5 := s.DeleteProvider(ctx, 1)
	_, err6 := s.ListModels(ctx)
	_, err7 := s.CreateModel(ctx, db.CreateModelParams{})
	err8 := s.UpdateModel(ctx, db.UpdateModelParams{})
	err9 := s.DeleteModel(ctx, 1)
	_, err10 := s.ListClientKeys(ctx)
	_, err11 := s.CreateClientKey(ctx, db.CreateClientKeyParams{})
	err12 := s.SetClientKeyEnabled(ctx, 1, true)
	_, err13 := s.ListSettings(ctx)
	_, err14 := s.GetSetting(ctx, "k")
	err15 := s.UpsertSetting(ctx, "k", "v")
	_, err16 := s.InsertRequests(ctx, []db.InsertRequestParams{{}})
	_, err17 := s.CountRequests(ctx)
	_, err18 := s.ListRecentRequests(ctx, 1)
	err19 := s.Refresh(ctx)

	for i, e := range []error{err1, err2, err3, err4, err5, err6, err7, err8, err9, err10,
		err11, err12, err13, err14, err15, err16, err17, err18, err19} {
		if e == nil {
			t.Fatalf("closed store: case %d error = nil, want error", i)
		}
	}
}
