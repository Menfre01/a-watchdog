package server

import (
	"context"
	"log/slog"

	"awatchdog/pkg/chain"
	"awatchdog/pkg/config"
	"awatchdog/pkg/middleware"
	"awatchdog/pkg/protocol"
)

// Wire 组装完整责任链:record(响应后处理)→ auth(准入)→ route(路由)→ 上游调用(终结点)。
// 入口装配与测试共用此函数(对应 design.md D3 的初始注册表)。
func Wire(ctx context.Context, store *config.Store, logger *slog.Logger) (chain.Handler, *middleware.Recorder, error) {
	recorder := middleware.NewRecorder(ctx, store, logger)
	registry := chain.NewRegistry()
	registrations := []struct {
		name  string
		stage chain.Stage
		ctor  chain.Constructor
	}{
		{"auth", chain.StageAdmission, func(string, map[string]string) (chain.Middleware, error) {
			return middleware.NewAuth(store.Snapshot), nil
		}},
		{"route", chain.StageRouting, func(string, map[string]string) (chain.Middleware, error) {
			return middleware.NewRoute(store.Snapshot), nil
		}},
		{"record", chain.StagePostprocess, func(string, map[string]string) (chain.Middleware, error) {
			return recorder, nil
		}},
	}
	for _, r := range registrations {
		if err := registry.Register(r.name, r.stage, r.ctor); err != nil {
			return nil, nil, err
		}
	}
	chainHandler, err := chain.Build(registry, []chain.MiddlewareConfig{
		{Name: "record", Enabled: true},
		{Name: "auth", Enabled: true},
		{Name: "route", Enabled: true},
	}, protocol.NewProxy().Handle)
	if err != nil {
		return nil, nil, err
	}
	return chainHandler, recorder, nil
}
