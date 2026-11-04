package config

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"awatchdog/pkg/config/db"
)

// newTestStore 在临时目录打开测试用 Store。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return s
}

// mustProvider 建一个测试用 provider。
func mustProvider(t *testing.T, s *Store, name string) db.Provider {
	t.Helper()
	p, err := s.CreateProvider(context.Background(), db.CreateProviderParams{
		Name:     name,
		Protocol: "openai-chat",
		BaseUrl:  "https://example.com",
		ApiKey:   "sk-test",
		Enabled:  1,
	})
	if err != nil {
		t.Fatalf("CreateProvider(%s) error = %v", name, err)
	}
	return p
}

func TestStore_ProviderCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p := mustProvider(t, s, "deepseek")
	if p.ID == 0 || p.Name != "deepseek" {
		t.Fatalf("created provider = %+v", p)
	}

	// 唯一约束:同名再建必须失败。
	dup := db.CreateProviderParams{Name: "deepseek", Protocol: "openai-chat", BaseUrl: "x", ApiKey: "y", Enabled: 1}
	if _, err := s.CreateProvider(ctx, dup); err == nil {
		t.Fatalf("duplicate provider name accepted, want unique violation")
	}

	if err := s.UpdateProvider(ctx, db.UpdateProviderParams{
		ID: p.ID, Name: "deepseek", Protocol: "openai-chat",
		BaseUrl: "https://new.example.com", ApiKey: "sk-new", Enabled: 1,
	}); err != nil {
		t.Fatalf("UpdateProvider() error = %v", err)
	}
	list, err := s.ListProviders(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListProviders() = %v, %v", list, err)
	}
	if list[0].BaseUrl != "https://new.example.com" {
		t.Fatalf("updated BaseUrl = %q", list[0].BaseUrl)
	}

	if err := s.DeleteProvider(ctx, p.ID); err != nil {
		t.Fatalf("DeleteProvider() error = %v", err)
	}
	if list, _ := s.ListProviders(ctx); len(list) != 0 {
		t.Fatalf("after delete, providers = %v", list)
	}
}

func TestStore_ModelCatalog(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := mustProvider(t, s, "anthropic-official")

	m, err := s.CreateModel(ctx, db.CreateModelParams{
		Name: "claude-sonnet-4-5", ProviderID: p.ID, UpstreamModel: "claude-sonnet-4-5", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateModel() error = %v", err)
	}
	if m.ID == 0 {
		t.Fatalf("created model = %+v", m)
	}

	// 引用完整性:不存在的 provider_id 必须被外键拒绝。
	ghost := db.CreateModelParams{Name: "ghost", ProviderID: 9999, UpstreamModel: "g", Enabled: 1}
	if _, err := s.CreateModel(ctx, ghost); err == nil {
		t.Fatalf("model with missing provider accepted, want FK violation")
	}
	// 唯一约束。
	dup := db.CreateModelParams{Name: "claude-sonnet-4-5", ProviderID: p.ID, UpstreamModel: "x", Enabled: 1}
	if _, err := s.CreateModel(ctx, dup); err == nil {
		t.Fatalf("duplicate model name accepted, want unique violation")
	}
	// 被引用时删除 provider 必须失败。
	if err := s.DeleteProvider(ctx, p.ID); err == nil {
		t.Fatalf("deleting referenced provider succeeded, want FK violation")
	}

	if err := s.UpdateModel(ctx, db.UpdateModelParams{
		ID: m.ID, Name: "claude-sonnet-4-5", ProviderID: p.ID, UpstreamModel: "claude-sonnet-4-5-v2", Enabled: 1,
	}); err != nil {
		t.Fatalf("UpdateModel() error = %v", err)
	}
	if err := s.DeleteModel(ctx, m.ID); err != nil {
		t.Fatalf("DeleteModel() error = %v", err)
	}
	if models, _ := s.ListModels(ctx); len(models) != 0 {
		t.Fatalf("after delete, models = %v", models)
	}
}

func TestStore_ClientKeys(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	k, err := s.CreateClientKey(ctx, db.CreateClientKeyParams{
		Name: "claude-code", KeyHash: "sha256:aaa", KeyPrefix: "awd_aaa", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateClientKey() error = %v", err)
	}
	if k.ID == 0 || k.Name != "claude-code" {
		t.Fatalf("created key = %+v", k)
	}

	info, ok := s.Snapshot().LookupKey("sha256:aaa")
	if !ok || !info.Enabled || info.Name != "claude-code" {
		t.Fatalf("snapshot key = %+v, %v", info, ok)
	}

	// 停用即时生效(快照即刻更新)。
	if err := s.SetClientKeyEnabled(ctx, k.ID, false); err != nil {
		t.Fatalf("SetClientKeyEnabled() error = %v", err)
	}
	if info, ok := s.Snapshot().LookupKey("sha256:aaa"); !ok || info.Enabled {
		t.Fatalf("停用未即时生效: %+v, %v", info, ok)
	}
}

func TestStore_Settings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertSetting(ctx, "upstream_timeout_ms", "3000"); err != nil {
		t.Fatalf("UpsertSetting(insert) error = %v", err)
	}
	if err := s.UpsertSetting(ctx, "upstream_timeout_ms", "5000"); err != nil {
		t.Fatalf("UpsertSetting(overwrite) error = %v", err)
	}
	v, err := s.GetSetting(ctx, "upstream_timeout_ms")
	if err != nil || v != "5000" {
		t.Fatalf("GetSetting() = %q, %v; want 5000", v, err)
	}
	if _, err := s.GetSetting(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSetting(missing) error = %v, want ErrNotFound", err)
	}
	if got, ok := s.Snapshot().Setting("upstream_timeout_ms"); !ok || got != "5000" {
		t.Fatalf("snapshot setting = %q, %v", got, ok)
	}
}

func TestStore_InsertRequests(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 空批为无操作。
	if _, err := s.InsertRequests(ctx, nil); err != nil {
		t.Fatalf("InsertRequests(nil) error = %v", err)
	}

	// 较大批次的写入(450 条):逐条 INSERT ... RETURNING id,验证批量路径可用。
	reqs := make([]db.InsertRequestParams, 0, 450)
	for i := 0; i < 450; i++ {
		reqs = append(reqs, db.InsertRequestParams{
			Ts:             "2026-01-01T00:00:00Z",
			ClientKeyName:  "claude-code",
			Protocol:       "anthropic-messages",
			RequestedModel: "claude-sonnet-4-5",
			StatusCode:     200,
			LatencyMs:      12,
			Stream:         1,
		})
	}
	in, out := int64(10), int64(20)
	reqs[0].UsageIn, reqs[0].UsageOut = &in, &out
	if _, err := s.InsertRequests(ctx, reqs); err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}

	var count int
	if err := s.sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM requests").Scan(&count); err != nil {
		t.Fatalf("count requests error = %v", err)
	}
	if count != 450 {
		t.Fatalf("requests count = %d, want 450", count)
	}

	var usageIn *int64
	row := s.sqlDB.QueryRowContext(ctx, "SELECT usage_in FROM requests WHERE id = (SELECT MIN(id) FROM requests)")
	if err := row.Scan(&usageIn); err != nil {
		t.Fatalf("scan usage_in error = %v", err)
	}
	if usageIn == nil || *usageIn != 10 {
		t.Fatalf("usage_in = %v, want 10", usageIn)
	}
}
