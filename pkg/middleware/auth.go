// Package middleware 实现网关内置的链上环节:鉴权(auth)、路由(route)
// 与请求记录(record)。环节在装配时以闭包注册进 chain.Registry(见 design.md D3)。
package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"awatchdog/pkg/chain"
	"awatchdog/pkg/config"
	"awatchdog/pkg/protocol"
)

// Auth 是准入阶段的鉴权环节:校验客户端凭据并写入身份。
type Auth struct {
	// snapshot 每请求调用一次以获取最新一致性快照(支持热刷新)。
	snapshot func() *config.Snapshot
}

// NewAuth 创建鉴权环节。
func NewAuth(snapshot func() *config.Snapshot) *Auth {
	return &Auth{snapshot: snapshot}
}

// Name 实现 chain.Middleware。
func (a *Auth) Name() string { return "auth" }

// Stage 实现 chain.Middleware。
func (a *Auth) Stage() chain.Stage { return chain.StageAdmission }

// Handle 实现 chain.Middleware:双凭据载体校验,失败返回受控 401。
func (a *Auth) Handle(ctx *chain.Context, next chain.Handler) error {
	key := bearerToken(ctx.R.Header.Get("Authorization"))
	if key == "" {
		key = ctx.R.Header.Get("x-api-key")
	}
	if key == "" {
		return respond(ctx.Protocol, http.StatusUnauthorized, "auth", "missing credentials")
	}
	info, ok := a.snapshot().LookupKey(config.HashClientKey(key))
	if !ok || !info.Enabled {
		return respond(ctx.Protocol, http.StatusUnauthorized, "auth", "invalid or disabled key")
	}
	ctx.ClientKey = &chain.ClientIdentity{KeyID: info.ID, KeyName: info.Name}
	return next(ctx)
}

// bearerToken 从 Authorization 头提取 Bearer 凭据(大小写不敏感)。
func bearerToken(h string) string {
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}

// respond 构造受控拒绝:按入向协议给出错误形状(对应 request-pipeline 的短路语义)。
func respond(protocolName string, status int, kind, message string) *chain.RejectError {
	if a, err := protocol.Get(protocolName); err == nil {
		contentType, body := a.ErrorResponse(status, kind, message)
		return &chain.RejectError{StatusCode: status, ContentType: contentType, Body: body, Kind: kind}
	}
	// 入向协议未注册属装配错误;退化为最简 JSON 体。
	body := fmt.Sprintf(`{"error":%q}`, message)
	return &chain.RejectError{StatusCode: status, ContentType: "application/json", Body: []byte(body), Kind: kind}
}
