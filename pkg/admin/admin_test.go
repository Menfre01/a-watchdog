package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
)

// testPassword 是管理面测试口令。
const testPassword = "test-admin-pass"

// newTestAdmin 创建管理面与测试服务器。
func newTestAdmin(t *testing.T) (*Admin, *config.Store, *httptest.Server) {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("config.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := New(store, testPassword, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	return a, store, srv
}

// do 发起带可选 CSRF 头与会话 cookie 的请求。
func do(t *testing.T, client *http.Client, method, url, body string, cookie *http.Cookie, withCSRF bool) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if withCSRF {
		req.Header.Set(csrfHeader, "1")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// decode 解析 JSON 响应体并关闭。
func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// readBody 读取并关闭响应体。
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// login 完成管理员登录并返回会话 cookie。
func login(t *testing.T, srv *httptest.Server) *http.Cookie {
	t.Helper()
	resp := do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/login",
		`{"password":"`+testPassword+`"}`, nil, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatalf("login 未返回会话 cookie")
	return nil
}

func TestAdmin_SessionLifecycle(t *testing.T) {
	_, _, srv := newTestAdmin(t)

	// 未登录访问 → 401。
	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/session", "", nil, false)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session(anon) = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 无 CSRF 头的登录 → 403。
	resp = do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/login",
		`{"password":"`+testPassword+`"}`, nil, false)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("login(no csrf) = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 错误口令 → 401。
	resp = do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/login", `{"password":"wrong"}`, nil, true)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login(wrong) = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 正确口令 → 会话可用。
	cookie := login(t, srv)
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/session", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session(authed) = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 登出:下发清除 cookie(无状态会话,清除即退出)。
	resp = do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/logout", "", cookie, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	cleared := false
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	_ = resp.Body.Close()
	if !cleared {
		t.Fatalf("logout 未清除会话 cookie")
	}
}

func TestAdmin_ProviderCRUD(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	// 创建。
	resp := do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/providers",
		`{"name":"deepseek","protocol":"openai-chat","base_url":"https://api.deepseek.com","api_key":"sk-up","enabled":true}`,
		cookie, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	var created struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	decode(t, resp, &created)
	if created.ID == 0 || created.Name != "deepseek" || !created.Enabled {
		t.Fatalf("created = %+v", created)
	}
	if _, ok := store.Snapshot().LookupProvider(created.ID); !ok {
		t.Fatalf("快照未包含新 provider(写后应即时刷新)")
	}

	// 未知协议 → 400。
	resp = do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/providers",
		`{"name":"x","protocol":"nope","base_url":"https://x","api_key":"k","enabled":true}`, cookie, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown protocol = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 重复名字 → 409。
	resp = do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/providers",
		`{"name":"deepseek","protocol":"openai-chat","base_url":"https://x","api_key":"k","enabled":true}`, cookie, true)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate name = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 更新。
	resp = do(t, srv.Client(), http.MethodPut,
		srv.URL+"/console/api/providers/"+strconv.FormatInt(created.ID, 10),
		`{"name":"deepseek","protocol":"openai-chat","base_url":"https://new.example.com","api_key":"sk-2","enabled":false}`,
		cookie, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update = %d", resp.StatusCode)
	}
	var updated struct {
		BaseURL string `json:"base_url"`
		Enabled bool   `json:"enabled"`
	}
	decode(t, resp, &updated)
	if updated.BaseURL != "https://new.example.com" || updated.Enabled {
		t.Fatalf("updated = %+v", updated)
	}

	// 删除。
	resp = do(t, srv.Client(), http.MethodDelete,
		srv.URL+"/console/api/providers/"+strconv.FormatInt(created.ID, 10), "", cookie, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestAdmin_ModelsKeysSettings(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	// 先建 provider。
	resp := do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/providers",
		`{"name":"p","protocol":"anthropic-messages","base_url":"https://api.anthropic.com","api_key":"sk","enabled":true}`,
		cookie, true)
	var prov struct {
		ID int64 `json:"id"`
	}
	decode(t, resp, &prov)

	// 建模型目录条目。
	resp = do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/models",
		fmt.Sprintf(`{"name":"claude-sonnet-4-5","provider_id":%d,"upstream_model":"claude-sonnet-4-5","enabled":true}`, prov.ID),
		cookie, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create model = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 删除被引用的 provider → 409。
	resp = do(t, srv.Client(), http.MethodDelete,
		srv.URL+"/console/api/providers/"+strconv.FormatInt(prov.ID, 10), "", cookie, true)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete referenced provider = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 生成客户端密钥:明文仅出现一次,且哈希立刻进入鉴权快照。
	resp = do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/client-keys", `{"name":"cli"}`, cookie, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create key = %d", resp.StatusCode)
	}
	var createdKey struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	decode(t, resp, &createdKey)
	if !strings.HasPrefix(createdKey.Key, "awd_") {
		t.Fatalf("generated key = %q", createdKey.Key)
	}
	if info, ok := store.Snapshot().LookupKey(config.HashClientKey(createdKey.Key)); !ok || !info.Enabled {
		t.Fatalf("新 key 未进入快照: %+v, %v", info, ok)
	}

	// 列表不包含明文。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/client-keys", "", cookie, false)
	var keys []map[string]any
	decode(t, resp, &keys)
	if len(keys) != 1 {
		t.Fatalf("keys = %v", keys)
	}
	if _, has := keys[0]["key"]; has {
		t.Fatalf("列表不应包含密钥明文")
	}

	// 停用:快照即时生效。
	resp = do(t, srv.Client(), http.MethodPut,
		srv.URL+"/console/api/client-keys/"+strconv.FormatInt(createdKey.ID, 10),
		`{"enabled":false}`, cookie, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("disable key = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if info, _ := store.Snapshot().LookupKey(config.HashClientKey(createdKey.Key)); info.Enabled {
		t.Fatalf("停用未即时生效")
	}

	// 删除:快照即时移除,列表清空;再删返回 404。
	resp = do(t, srv.Client(), http.MethodDelete,
		srv.URL+"/console/api/client-keys/"+strconv.FormatInt(createdKey.ID, 10), "", cookie, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete key = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if _, ok := store.Snapshot().LookupKey(config.HashClientKey(createdKey.Key)); ok {
		t.Fatalf("删除未即时生效")
	}
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/client-keys", "", cookie, false)
	decode(t, resp, &keys)
	if len(keys) != 0 {
		t.Fatalf("删除后列表 = %v", keys)
	}
	resp = do(t, srv.Client(), http.MethodDelete,
		srv.URL+"/console/api/client-keys/"+strconv.FormatInt(createdKey.ID, 10), "", cookie, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("重复删除 = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// settings 写入 + 列表。
	resp = do(t, srv.Client(), http.MethodPut, srv.URL+"/console/api/settings/upstream_timeout_ms",
		`{"value":"3000"}`, cookie, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upsert setting = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/settings", "", cookie, false)
	var settings []map[string]string
	decode(t, resp, &settings)
	if len(settings) != 1 || settings[0]["key"] != "upstream_timeout_ms" || settings[0]["value"] != "3000" {
		t.Fatalf("settings = %v", settings)
	}
}

func TestAdmin_ProbeProvider(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	// 正常上游:以「测试模型」发起最小请求(POST /v1/messages,1 token 输出)且带凭据头。
	okUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/messages" || r.Header.Get("X-Api-Key") != "sk-probe" ||
			!strings.Contains(string(body), `"max_tokens":1`) || !strings.Contains(string(body), `"model":"probe-model"`) {
			t.Errorf("probe request = %s key=%q body=%s", r.URL.Path, r.Header.Get("X-Api-Key"), body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1"}`)
	}))
	defer okUpstream.Close()
	p, err := store.CreateProvider(context.Background(), db.CreateProviderParams{
		Name: "ok", Protocol: "anthropic-messages", BaseUrl: okUpstream.URL, ApiKey: "sk-probe",
		ProbeModel: "probe-model", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	resp := do(t, srv.Client(), http.MethodPost,
		srv.URL+"/console/api/providers/"+strconv.FormatInt(p.ID, 10)+"/test", "", cookie, true)
	var result struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	decode(t, resp, &result)
	if !result.OK || !strings.Contains(result.Detail, "model=probe-model") {
		t.Fatalf("probe(ok upstream) = %+v", result)
	}

	// 失败上游:500 → 可读原因(含状态码与错误体片段)。
	failUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failUpstream.Close()
	p2, err := store.CreateProvider(context.Background(), db.CreateProviderParams{
		Name: "fail", Protocol: "openai-chat", BaseUrl: failUpstream.URL, ApiKey: "sk",
		ProbeModel: "m", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	resp = do(t, srv.Client(), http.MethodPost,
		srv.URL+"/console/api/providers/"+strconv.FormatInt(p2.ID, 10)+"/test", "", cookie, true)
	var failed struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
		Status int    `json:"status"`
	}
	decode(t, resp, &failed)
	if failed.OK || failed.Status != http.StatusInternalServerError ||
		!strings.Contains(failed.Detail, "500") || !strings.Contains(failed.Detail, "boom") {
		t.Fatalf("probe(fail upstream) = %+v", failed)
	}
}

func TestAdmin_StaticAndRedirect(t *testing.T) {
	_, _, srv := newTestAdmin(t)

	// /console → 302 跳转到 /console/。
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp := do(t, client, http.MethodGet, srv.URL+"/console", "", nil, false)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("redirect = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 内嵌前端:产物未构建时给出可读提示;已构建时返回页面(两种状态都接受)。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/", "", nil, false)
	index := readBody(t, resp)
	switch resp.StatusCode {
	case http.StatusServiceUnavailable:
		if !strings.Contains(index, "未构建") {
			t.Fatalf("placeholder page = %q", index)
		}
	case http.StatusOK:
		if !strings.Contains(index, `<div id="app">`) {
			t.Fatalf("index page = %q", index)
		}
	default:
		t.Fatalf("console page status = %d", resp.StatusCode)
	}
}
