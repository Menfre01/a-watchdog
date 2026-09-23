package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdmin_UpdateValidationBranches(t *testing.T) {
	_, _, srv := newTestAdmin(t)
	cookie := login(t, srv)

	cases := []struct{ method, path, body string }{
		{http.MethodPut, "/console/api/providers/1", `not-json`},
		{http.MethodPut, "/console/api/providers/1", `{}`},
		{http.MethodPut, "/console/api/providers/1", `{"name":"n","protocol":"nope","base_url":"https://x","api_key":"k","enabled":true}`},
		{http.MethodPut, "/console/api/models/1", `not-json`},
		{http.MethodPut, "/console/api/models/1", `{}`},
		{http.MethodPut, "/console/api/client-keys/1", `not-json`},
		{http.MethodPut, "/console/api/client-keys/1", `{"enabled":false}{"enabled":true}`},
	}
	for _, c := range cases {
		resp := do(t, srv.Client(), c.method, srv.URL+c.path, c.body, cookie, true)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s %s body=%q = %d, want 400", c.method, c.path, c.body, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

func TestAdmin_ListProvidersAfterCreate(t *testing.T) {
	_, _, srv := newTestAdmin(t)
	cookie := login(t, srv)

	resp := do(t, srv.Client(), http.MethodPost, srv.URL+"/console/api/providers",
		`{"name":"listed","protocol":"openai-chat","base_url":"https://x.example.com","api_key":"sk","enabled":true}`,
		cookie, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/providers", "", cookie, false)
	var rows []map[string]any
	decode(t, resp, &rows)
	if len(rows) != 1 || rows[0]["name"] != "listed" {
		t.Fatalf("providers = %v", rows)
	}
}

func TestAdmin_UpsertSetting_EmptyKey(t *testing.T) {
	a, _, _ := newTestAdmin(t)
	// 路由参数缺失(等价 {key} 为空)时直接调用处理器,覆盖 key 校验分支。
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/console/api/settings/", nil)
	rec := httptest.NewRecorder()
	a.handleUpsertSetting(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty key = %d, want 400", rec.Code)
	}
}
