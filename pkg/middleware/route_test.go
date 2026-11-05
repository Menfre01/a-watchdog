package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"awatchdog/pkg/chain"
	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
)

// setupCatalog 建一个 provider 与其模型条目,返回两者的 id。
func setupCatalog(t *testing.T, s *config.Store) (providerID, modelID int64) {
	t.Helper()
	p, err := s.CreateProvider(context.Background(), db.CreateProviderParams{
		Name: "anthropic-official", Protocol: "anthropic-messages",
		BaseUrl: "https://api.anthropic.com", ApiKey: "sk-ant-upstream", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	m, err := s.CreateModel(context.Background(), db.CreateModelParams{
		Name: "claude-sonnet-4-5", ProviderID: p.ID, UpstreamModel: "claude-sonnet-4-5-v2", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateModel() error = %v", err)
	}
	return p.ID, m.ID
}

func TestRoute_Hit(t *testing.T) {
	store := newStore(t)
	providerID, _ := setupCatalog(t, store)
	mw := NewRoute(store.Snapshot)

	called := false
	ctx := newCtx("anthropic-messages", nil, `{"model":"claude-sonnet-4-5","messages":[]}`)
	if err := mw.Handle(ctx, okHandler(&called)); err != nil || !called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
	route := ctx.Route
	if route == nil {
		t.Fatalf("ctx.Route = nil")
		return
	}
	if route.ClientModel != "claude-sonnet-4-5" || route.ProviderID != providerID ||
		route.ProviderProtocol != "anthropic-messages" || route.BaseURL != "https://api.anthropic.com" ||
		route.APIKey != "sk-ant-upstream" || route.UpstreamModel != "claude-sonnet-4-5-v2" {
		t.Fatalf("route = %+v", route)
	}
}

func TestRoute_Miss(t *testing.T) {
	store := newStore(t)
	setupCatalog(t, store)
	mw := NewRoute(store.Snapshot)

	called := false
	ctx := newCtx("anthropic-messages", nil, `{"model":"unknown-model","messages":[]}`)
	err := mw.Handle(ctx, okHandler(&called))
	var rejected *chain.RejectError
	if !errors.As(err, &rejected) || rejected.StatusCode != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if rejected.Kind != "model_not_found" || !strings.Contains(string(rejected.Body), "not_found_error") {
		t.Fatalf("reject = %+v body=%s", rejected, rejected.Body)
	}
	if called || ctx.Route != nil {
		t.Fatalf("未命中不应写入路由或进入下游")
	}
}

func TestRoute_InvalidBody(t *testing.T) {
	store := newStore(t)
	mw := NewRoute(store.Snapshot)
	called := false
	err := mw.Handle(newCtx("openai-chat", nil, `not-json`), okHandler(&called))
	var rejected *chain.RejectError
	if !errors.As(err, &rejected) || rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("err = %v, want 400", err)
	}
	if called {
		t.Fatalf("下游不应被调用")
	}
}

func TestRoute_DisabledModelOrProvider(t *testing.T) {
	ctxbg := context.Background()
	store := newStore(t)
	providerID, modelID := setupCatalog(t, store)
	mw := NewRoute(store.Snapshot)
	body := `{"model":"claude-sonnet-4-5","messages":[]}`

	// 模型停用 → 404。
	if err := store.UpdateModel(ctxbg, db.UpdateModelParams{
		ID: modelID, Name: "claude-sonnet-4-5", ProviderID: providerID,
		UpstreamModel: "claude-sonnet-4-5-v2", Enabled: 0,
	}); err != nil {
		t.Fatalf("UpdateModel() error = %v", err)
	}
	called := false
	var rejected *chain.RejectError
	if err := mw.Handle(newCtx("anthropic-messages", nil, body), okHandler(&called)); !errors.As(err, &rejected) ||
		rejected.StatusCode != http.StatusNotFound || called {
		t.Fatalf("模型停用后 err = %v, called = %v", err, called)
	}

	// 恢复模型、停用 provider → 404。
	if err := store.UpdateModel(ctxbg, db.UpdateModelParams{
		ID: modelID, Name: "claude-sonnet-4-5", ProviderID: providerID,
		UpstreamModel: "claude-sonnet-4-5-v2", Enabled: 1,
	}); err != nil {
		t.Fatalf("UpdateModel(restore) error = %v", err)
	}
	if err := store.UpdateProvider(ctxbg, db.UpdateProviderParams{
		ID: providerID, Name: "anthropic-official", Protocol: "anthropic-messages",
		BaseUrl: "https://api.anthropic.com", ApiKey: "sk-ant-upstream", Enabled: 0,
	}); err != nil {
		t.Fatalf("UpdateProvider() error = %v", err)
	}
	called = false
	if err := mw.Handle(newCtx("anthropic-messages", nil, body), okHandler(&called)); !errors.As(err, &rejected) ||
		rejected.StatusCode != http.StatusNotFound || called {
		t.Fatalf("provider 停用后 err = %v, called = %v", err, called)
	}
}
