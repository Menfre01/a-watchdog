package admin

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"ds-context/pkg/config/db"
)

func TestValidSession_Variants(t *testing.T) {
	a, _, _ := newTestAdmin(t)
	mk := func(value string) *http.Request {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/console/api/session", nil)
		if value != "" {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: value})
		}
		return req
	}

	if !a.validSession(mk(a.newSessionToken(time.Now()))) {
		t.Fatalf("有效会话被判无效")
	}
	if a.validSession(mk("")) {
		t.Fatalf("无 cookie 应为无效")
	}
	if a.validSession(mk("no-dot")) {
		t.Fatalf("缺少分隔符应为无效")
	}
	if a.validSession(mk("123.not-hex")) {
		t.Fatalf("非法签名编码应为无效")
	}
	if a.validSession(mk("123." + strings.Repeat("ab", 32))) {
		t.Fatalf("错误签名应为无效")
	}
	if a.validSession(mk(a.newSessionToken(time.Now().Add(-13 * time.Hour)))) {
		t.Fatalf("过期会话应为无效")
	}
	if a.validSession(mk("not-a-number." + strings.Repeat("ab", 32))) {
		t.Fatalf("非法时间戳应为无效")
	}
}

func TestAdmin_LoginBranches(t *testing.T) {
	_, store, srv := newTestAdmin(t)

	// 坏请求体 → 400。
	resp := do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/login", `not-json`, nil, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("login(bad body) = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 未配置口令 → 401。
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a2, err := New(store, "", logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	srv2 := httptest.NewServer(a2.Handler())
	defer srv2.Close()
	resp = do(t, srv2.Client(), http.MethodPost, srv2.URL+"/console/api/login", `{"password":"x"}`, nil, true)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login(no password configured) = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestAdmin_ListsAndModelCRUD(t *testing.T) {
	_, _, srv := newTestAdmin(t)
	cookie := login(t, srv)

	// 空库:四个列表都可读。
	for _, path := range []string{
		"/console/api/providers", "/console/api/models", "/console/api/client-keys", "/console/api/settings",
	} {
		resp := do(t, srv.Client(), http.MethodGet, srv.URL+path, "", cookie, false)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// 建 provider(API 校验通过)与模型条目。
	resp := do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/providers",
		`{"name":"p","protocol":"openai-chat","base_url":"https://x.example.com","api_key":"sk","enabled":true}`,
		cookie, true)
	var prov struct {
		ID int64 `json:"id"`
	}
	decode(t, resp, &prov)

	resp = do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/models",
		`{"name":"m1","provider_id":`+strconv.FormatInt(prov.ID, 10)+`,"upstream_model":"um","enabled":true}`,
		cookie, true)
	var model struct {
		ID int64 `json:"id"`
	}
	decode(t, resp, &model)

	// 列表非空。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/models", "", cookie, false)
	var models []map[string]any
	decode(t, resp, &models)
	if len(models) != 1 {
		t.Fatalf("models = %v", models)
	}

	// 更新模型。
	resp = do(t, srv.Client(), http.MethodPut, srv.URL+"/console/api/models/"+strconv.FormatInt(model.ID, 10),
		`{"name":"m1","provider_id":`+strconv.FormatInt(prov.ID, 10)+`,"upstream_model":"um2","enabled":false}`,
		cookie, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("update model = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 删除模型后删除 provider。
	resp = do(t, srv.Client(), http.MethodDelete, srv.URL+"/console/api/models/"+strconv.FormatInt(model.ID, 10),
		"", cookie, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete model = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = do(t, srv.Client(), http.MethodDelete, srv.URL+"/console/api/providers/"+strconv.FormatInt(prov.ID, 10),
		"", cookie, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete provider = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestAdmin_ValidationAndErrors(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	ctxbg := context.Background()

	// 路径 id 非法 → 400。
	badID := []struct {
		method, path, body string
	}{
		{http.MethodPut, "/console/api/providers/abc", `{"name":"n","protocol":"openai-chat","base_url":"https://x","api_key":"k","enabled":true}`},
		{http.MethodDelete, "/console/api/providers/0", ""},
		{http.MethodPost, "/console/api/providers/abc/test", ""},
		{http.MethodPut, "/console/api/models/abc", `{"name":"n","provider_id":1,"upstream_model":"u","enabled":true}`},
		{http.MethodDelete, "/console/api/models/0", ""},
		{http.MethodPut, "/console/api/client-keys/abc", `{"enabled":true}`},
	}
	for _, c := range badID {
		resp := do(t, srv.Client(), c.method, srv.URL+c.path, c.body, cookie, true)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s %s = %d, want 400", c.method, c.path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// 缺字段 / 坏请求体 → 400。
	badReq := []struct {
		method, path, body string
	}{
		{http.MethodPost, "/console/api/providers", `{}`},
		{http.MethodPost, "/console/api/providers", `not-json`},
		{http.MethodPost, "/console/api/models", `{}`},
		{http.MethodPost, "/console/api/models", `{"name":"m","provider_id":`},
		{http.MethodPost, "/console/api/client-keys", `{}`},
		{http.MethodPost, "/console/api/client-keys", `not-json`},
		{http.MethodPut, "/console/api/settings/k", `not-json`},
	}
	for _, c := range badReq {
		resp := do(t, srv.Client(), c.method, srv.URL+c.path, c.body, cookie, true)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s %s body=%q = %d, want 400", c.method, c.path, c.body, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// 更新不存在的 provider → 404(storeErr 的 ErrNotFound 分支)。
	resp := do(t, srv.Client(), http.MethodPut, srv.URL+"/console/api/providers/999",
		`{"name":"n","protocol":"openai-chat","base_url":"https://x","api_key":"k","enabled":true}`, cookie, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("update missing provider = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 引用不存在的 provider 建模型 → 409(外键冲突)。
	resp = do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/models",
		`{"name":"m","provider_id":999,"upstream_model":"u","enabled":true}`, cookie, true)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("model with missing provider = %d, want 409", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 测试连接:不存在的 id → 404。
	resp = do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/providers/999/test", "", cookie, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("test missing provider = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 测试连接:上游不可达(连接拒绝)→ 可读失败。
	p, err := store.CreateProvider(ctxbg, db.CreateProviderParams{
		Name: "refused", Protocol: "openai-chat", BaseUrl: "http://127.0.0.1:1", ApiKey: "sk",
		ProbeModel: "m", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	resp = do(t, srv.Client(), http.MethodPost,
		srv.URL+"/console/api/providers/"+strconv.FormatInt(p.ID, 10)+"/test", "", cookie, true)
	var refused struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	decode(t, resp, &refused)
	if refused.OK || refused.Detail == "" {
		t.Fatalf("probe refused = %+v", refused)
	}

	// 测试连接:未知协议(绕过 API 校验,直接入库)→ 可读失败。
	pu, err := store.CreateProvider(ctxbg, db.CreateProviderParams{
		Name: "weird", Protocol: "no-such-protocol", BaseUrl: "http://127.0.0.1:1", ApiKey: "sk", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	resp = do(t, srv.Client(), http.MethodPost,
		srv.URL+"/console/api/providers/"+strconv.FormatInt(pu.ID, 10)+"/test", "", cookie, true)
	var weird struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	decode(t, resp, &weird)
	if weird.OK || !strings.Contains(weird.Detail, "unknown protocol") {
		t.Fatalf("probe unknown protocol = %+v", weird)
	}

	// 重定向上游:探测不跟随(防凭据经 3xx 外泄),按非 2xx 报告。
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("重定向目标不应被探测访问")
	}))
	defer redirectTarget.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer redirector.Close()
	p3, err := store.CreateProvider(ctxbg, db.CreateProviderParams{
		Name: "redirect", Protocol: "openai-chat", BaseUrl: redirector.URL, ApiKey: "sk",
		ProbeModel: "m", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	resp = do(t, srv.Client(), http.MethodPost,
		srv.URL+"/console/api/providers/"+strconv.FormatInt(p3.ID, 10)+"/test", "", cookie, true)
	var redirected struct {
		OK     bool `json:"ok"`
		Status int  `json:"status"`
	}
	decode(t, resp, &redirected)
	if redirected.OK || redirected.Status != http.StatusFound {
		t.Fatalf("probe(redirect upstream) = %+v, want 302 不跟随", redirected)
	}
}

func TestStaticHandler_Branches(t *testing.T) {
	// 无 index.html → 503 提示页。
	h := newStaticHandler(fstest.MapFS{"placeholder.txt": &fstest.MapFile{Data: []byte("x")}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/console/", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "未构建") {
		t.Fatalf("no-index = %d %q", rec.Code, rec.Body.String())
	}

	// 有 index.html:根路径、SPA 回退、静态资源命中。
	fsys := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte(`<div id="app">ok`)},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	h2 := newStaticHandler(fsys)
	check := func(path string, wantCode int, wantSub string) {
		t.Helper()
		w := httptest.NewRecorder()
		h2.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
		if w.Code != wantCode || !strings.Contains(w.Body.String(), wantSub) {
			t.Fatalf("GET %s = %d %q, want %d contains %q", path, w.Code, w.Body.String(), wantCode, wantSub)
		}
	}
	check("/console/", http.StatusOK, `<div id="app">`)
	check("/console/spa/route", http.StatusOK, `<div id="app">`)
	check("/console/assets/app.js", http.StatusOK, "console.log(1)")
}
