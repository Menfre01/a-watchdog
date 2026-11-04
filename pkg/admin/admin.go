// Package admin 提供控制台管理面:管理员会话、配置 CRUD、连通性测试与内嵌前端
// (行为契约见 specs/admin-console;设计要求见 design.md D7)。
package admin

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"ds-context/pkg/config"
	"ds-context/pkg/trace"
)

// probeTimeout 是「测试连接」的最长等待时间。
const probeTimeout = 10 * time.Second

// Admin 是控制台管理面的入口。
type Admin struct {
	store    *config.Store
	password string
	secret   []byte
	logger   *slog.Logger
	client   *http.Client
	// traces 缓存会话级推断结果:键含最后一轮 request_id,有新轮次即自动失效。
	traces *trace.Cache[*sessionAnalysis]
}

// New 创建管理面:secret 用于会话签名(启动时随机生成,重启后旧会话失效)。
func New(store *config.Store, password string, logger *slog.Logger) (*Admin, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("admin: session secret: %w", err)
	}
	return &Admin{
		store:    store,
		password: password,
		secret:   secret,
		logger:   logger,
		client: &http.Client{
			Timeout: probeTimeout,
			// 探测同样禁止跟随重定向:避免凭据经 3xx 外泄到其他主机。
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		traces: trace.NewCache[*sessionAnalysis](trace.CacheDefaultTTL),
	}, nil
}

// Handler 返回控制台的全部路由(API + 内嵌前端)。
func (a *Admin) Handler() http.Handler {
	mux := http.NewServeMux()

	// 会话:登录只需 CSRF 头(尚无会话),其余需要管理员会话。
	mux.HandleFunc("POST /console/api/login", a.csrfGuard(a.handleLogin))
	mux.HandleFunc("POST /console/api/logout", a.csrfGuard(a.guard(a.handleLogout)))
	mux.HandleFunc("GET /console/api/session", a.guard(a.handleSession))

	// providers(接入)。
	mux.HandleFunc("GET /console/api/providers", a.guard(a.handleListProviders))
	mux.HandleFunc("POST /console/api/providers", a.csrfGuard(a.guard(a.handleCreateProvider)))
	mux.HandleFunc("PUT /console/api/providers/{id}", a.csrfGuard(a.guard(a.handleUpdateProvider)))
	mux.HandleFunc("DELETE /console/api/providers/{id}", a.csrfGuard(a.guard(a.handleDeleteProvider)))
	mux.HandleFunc("POST /console/api/providers/{id}/test", a.csrfGuard(a.guard(a.handleTestProvider)))

	// models(模型目录)。
	mux.HandleFunc("GET /console/api/models", a.guard(a.handleListModels))
	mux.HandleFunc("POST /console/api/models", a.csrfGuard(a.guard(a.handleCreateModel)))
	mux.HandleFunc("PUT /console/api/models/{id}", a.csrfGuard(a.guard(a.handleUpdateModel)))
	mux.HandleFunc("DELETE /console/api/models/{id}", a.csrfGuard(a.guard(a.handleDeleteModel)))

	// client keys(客户端密钥)。
	mux.HandleFunc("GET /console/api/client-keys", a.guard(a.handleListClientKeys))
	mux.HandleFunc("POST /console/api/client-keys", a.csrfGuard(a.guard(a.handleCreateClientKey)))
	mux.HandleFunc("PUT /console/api/client-keys/{id}", a.csrfGuard(a.guard(a.handleUpdateClientKey)))
	mux.HandleFunc("DELETE /console/api/client-keys/{id}", a.csrfGuard(a.guard(a.handleDeleteClientKey)))

	// settings(全局配置)。
	mux.HandleFunc("GET /console/api/settings", a.guard(a.handleListSettings))
	mux.HandleFunc("PUT /console/api/settings/{key}", a.csrfGuard(a.guard(a.handleUpsertSetting)))

	// 可观测(轨迹):全部只读,不修改任何数据。
	mux.HandleFunc("GET /console/api/traces", a.guard(a.handleListTraces))
	mux.HandleFunc("GET /console/api/traces/{session_id}", a.guard(a.handleTraceDetail))
	mux.HandleFunc("GET /console/api/traces/{session_id}/digest", a.guard(a.handleTraceDigest))
	mux.HandleFunc("GET /console/api/traces/{session_id}/{seq}", a.guard(a.handleTraceStep))
	mux.HandleFunc("GET /console/api/tool-events", a.guard(a.handleListToolEvents))
	mux.HandleFunc("GET /console/api/metrics", a.guard(a.handleRequestMetrics))
	mux.HandleFunc("GET /console/api/prompt-events", a.guard(a.handleListPromptEvents))

	// 内嵌前端(SPA 回退)。
	mux.Handle("/console/", a.staticHandler())
	mux.HandleFunc("GET /console", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/console/", http.StatusFound)
	})
	return mux
}

// guard 校验管理员会话,未通过时不泄露任何配置数据。
func (a *Admin) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.validSession(r) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

// csrfGuard 要求请求携带自定义头(CSRF 缓解)。
func (a *Admin) csrfGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(csrfHeader) == "" {
			writeJSONError(w, http.StatusForbidden, "missing csrf header")
			return
		}
		next(w, r)
	}
}
