package middleware

import (
	"fmt"
	"net/http"

	"ds-context/pkg/chain"
	"ds-context/pkg/config"
	"ds-context/pkg/protocol"
)

// Route 是路由/适配阶段的环节:按模型目录把请求 model 解析为上游目标。
type Route struct {
	// snapshot 每请求调用一次以获取最新一致性快照(支持热刷新)。
	snapshot func() *config.Snapshot
}

// NewRoute 创建路由环节。
func NewRoute(snapshot func() *config.Snapshot) *Route {
	return &Route{snapshot: snapshot}
}

// Name 实现 chain.Middleware。
func (r *Route) Name() string { return "route" }

// Stage 实现 chain.Middleware。
func (r *Route) Stage() chain.Stage { return chain.StageRouting }

// Handle 实现 chain.Middleware:命中则写入上游决策;未命中返回协议形状的 404,
// 且不发起任何上游调用(对应 protocol-routing spec「未命中错误」)。
func (r *Route) Handle(ctx *chain.Context, next chain.Handler) error {
	adapter, err := protocol.Get(ctx.Protocol)
	if err != nil {
		return err // 装配错误:暴露为内部错误
	}
	model, err := adapter.ParseModel(ctx.Body)
	if err != nil {
		return respond(ctx.Protocol, http.StatusBadRequest, "invalid_request", "invalid request body")
	}
	snap := r.snapshot()
	entry, ok := snap.LookupModel(model)
	if !ok || !entry.Enabled {
		return respond(ctx.Protocol, http.StatusNotFound, "model_not_found",
			fmt.Sprintf("model %q is not configured", model))
	}
	provider, ok := snap.LookupProvider(entry.ProviderID)
	if !ok || !provider.Enabled {
		return respond(ctx.Protocol, http.StatusNotFound, "model_not_found",
			fmt.Sprintf("model %q is not available", model))
	}
	ctx.Route = &chain.RouteDecision{
		ClientModel:      model,
		ProviderID:       provider.ID,
		ProviderName:     provider.Name,
		ProviderProtocol: provider.Protocol,
		BaseURL:          provider.BaseURL,
		APIKey:           provider.APIKey,
		UpstreamModel:    entry.UpstreamModel,
	}
	return next(ctx)
}
