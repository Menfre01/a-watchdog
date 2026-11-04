package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPromptEventsIngest 覆盖提示提交上报:鉴权、长度记录与幂等。
func TestPromptEventsIngest(t *testing.T) {
	store, handler := newTestStack(t, upstreamOK(t).URL)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	url := srv.URL + "/v1/prompt-events?client=claude-code"
	headers := map[string]string{"Authorization": "Bearer " + testClientKey}
	body := `{"hook_event_name":"UserPromptSubmit","session_id":"p1","prompt_id":"pr1","prompt":"帮我看看登录接口"}`

	for i := 0; i < 2; i++ {
		resp := postJSON(t, srv.Client(), url, body, headers)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("上报 = %d, want 204", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	rows, err := store.ListPromptEvents(context.Background(), "1:p1")
	if err != nil {
		t.Fatalf("ListPromptEvents() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("事件数 = %d, want 1(重复 prompt_id 被忽略)", len(rows))
	}
	if rows[0].PromptLen != int64(len("帮我看看登录接口")) {
		t.Errorf("提示长度 = %d", rows[0].PromptLen)
	}

	resp := postJSON(t, srv.Client(), url, body, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无凭据 = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
