package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds-context/pkg/config/db"
)

// TestAdmin_StoreErrorBranches 关闭存储后调用各管理端点,覆盖存储错误分支(统一 500)。
func TestAdmin_StoreErrorBranches(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	cases := []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/console/api/providers", "", http.StatusInternalServerError},
		{http.MethodGet, "/console/api/models", "", http.StatusInternalServerError},
		{http.MethodGet, "/console/api/client-keys", "", http.StatusInternalServerError},
		{http.MethodGet, "/console/api/settings", "", http.StatusInternalServerError},
		{http.MethodPost, "/console/api/providers", `{"name":"n","protocol":"openai-chat","base_url":"https://x","api_key":"k","enabled":true}`, http.StatusInternalServerError},
		{http.MethodPut, "/console/api/providers/1", `{"name":"n","protocol":"openai-chat","base_url":"https://x","api_key":"k","enabled":true}`, http.StatusInternalServerError},
		{http.MethodDelete, "/console/api/providers/1", "", http.StatusInternalServerError},
		{http.MethodPost, "/console/api/providers/1/test", "", http.StatusInternalServerError},
		{http.MethodPost, "/console/api/models", `{"name":"m","provider_id":1,"upstream_model":"u","enabled":true}`, http.StatusInternalServerError},
		{http.MethodPut, "/console/api/models/1", `{"name":"m","provider_id":1,"upstream_model":"u","enabled":true}`, http.StatusInternalServerError},
		{http.MethodDelete, "/console/api/models/1", "", http.StatusInternalServerError},
		{http.MethodPost, "/console/api/client-keys", `{"name":"cli"}`, http.StatusInternalServerError},
		{http.MethodPut, "/console/api/client-keys/1", `{"enabled":false}`, http.StatusInternalServerError},
		{http.MethodDelete, "/console/api/client-keys/1", "", http.StatusInternalServerError},
		{http.MethodPut, "/console/api/settings/k", `{"value":"v"}`, http.StatusInternalServerError},
	}
	for _, c := range cases {
		resp := do(t, srv.Client(), c.method, srv.URL+c.path, c.body, cookie, true)
		if resp.StatusCode != c.want {
			t.Fatalf("%s %s = %d, want %d", c.method, c.path, resp.StatusCode, c.want)
		}
		_ = resp.Body.Close()
	}
}

func TestValidSession_NonNumericExpiry(t *testing.T) {
	a, _, _ := newTestAdmin(t)
	// 签名正确但过期时间非数字:必须走到 ParseInt 失败分支。
	exp := "not-a-number"
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(exp))
	token := exp + "." + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/console/api/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	if a.validSession(req) {
		t.Fatalf("非数字过期时间应为无效会话")
	}
}

func TestProbeProvider_InvalidBaseURL(t *testing.T) {
	a, store, _ := newTestAdmin(t)
	// 无法解析的 URL → NewRequest 失败分支。
	p, err := store.CreateProvider(context.Background(), db.CreateProviderParams{
		Name: "bad-url", Protocol: "openai-chat", BaseUrl: "http://[::1", ApiKey: "sk",
		ProbeModel: "m", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	res := a.probeProvider(context.Background(), p)
	if res.OK || res.Detail == "" || strings.Contains(res.Detail, "未配置") {
		t.Fatalf("probe(invalid url) = %+v, want URL 解析错误", res)
	}
}

func TestProbeProvider_MissingProbeModel(t *testing.T) {
	a, store, _ := newTestAdmin(t)
	// 未配置「测试模型」:必须明确提示,且不发起上游调用(上游地址设为不可达也能通过)。
	p, err := store.CreateProvider(context.Background(), db.CreateProviderParams{
		Name: "no-model", Protocol: "openai-chat", BaseUrl: "http://127.0.0.1:1", ApiKey: "sk", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	res := a.probeProvider(context.Background(), p)
	if res.OK || !strings.Contains(res.Detail, "未配置测试模型") {
		t.Fatalf("probe(missing probe model) = %+v", res)
	}
}
