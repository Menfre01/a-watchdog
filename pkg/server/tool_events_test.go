package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestToolEventsIngest 覆盖上报端点:适配 Claude Code 载荷、幂等入库与鉴权。
func TestToolEventsIngest(t *testing.T) {
	store, handler := newTestStack(t, upstreamOK(t).URL)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	url := srv.URL + "/v1/tool-events?client=claude-code"
	headers := map[string]string{"Authorization": "Bearer " + testClientKey}

	cases := []struct {
		body string
		want int
	}{
		{`{"hook_event_name":"PreToolUse","session_id":"s1","tool_use_id":"t1","tool_name":"Bash","tool_input":{"command":"ls"}}`, http.StatusNoContent},
		{`{"hook_event_name":"PostToolUse","session_id":"s1","tool_use_id":"t1","tool_name":"Bash","tool_response":{"stdout":"a.txt"}}`, http.StatusNoContent},
		// 同一 phase 重复上报:幂等,不新增行。
		{`{"hook_event_name":"PostToolUse","session_id":"s1","tool_use_id":"t1","tool_name":"Bash","tool_response":{"stdout":"a.txt"}}`, http.StatusNoContent},
		{`{"hook_event_name":"PostToolUseFailure","session_id":"s1","tool_use_id":"t2","tool_name":"Bash"}`, http.StatusNoContent},
	}
	for _, c := range cases {
		resp := postJSON(t, srv.Client(), url, c.body, headers)
		if resp.StatusCode != c.want {
			t.Fatalf("上报 %s = %d, want %d", c.body, resp.StatusCode, c.want)
		}
		_ = resp.Body.Close()
	}

	rows, err := store.ListToolEvents(context.Background(), "1:s1")
	if err != nil {
		t.Fatalf("ListToolEvents() error = %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("事件数 = %d, want 3(重复的 end 被忽略)", len(rows))
	}
	if rows[0].Phase != "start" || rows[0].Boundary != "hook" {
		t.Errorf("首条 = %+v, want start/hook", rows[0])
	}
	last := rows[len(rows)-1]
	if last.Phase != "end" || last.Status != "error" {
		t.Errorf("失败事件 = %+v, want end/error", last)
	}
	if len(rows[0].InputGzip) == 0 || len(rows[1].ResultGzip) == 0 {
		t.Errorf("入参或结果未落库: start=%d end=%d", len(rows[0].InputGzip), len(rows[1].ResultGzip))
	}
}

// TestToolEventsAuthAndValidation 覆盖鉴权与载荷校验。
func TestToolEventsAuthAndValidation(t *testing.T) {
	_, handler := newTestStack(t, upstreamOK(t).URL)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	url := srv.URL + "/v1/tool-events?client=claude-code"
	body := `{"hook_event_name":"PreToolUse","session_id":"s1","tool_use_id":"t1","tool_name":"Bash","tool_input":{"command":"ls"}}`

	resp := postJSON(t, srv.Client(), url, body, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无凭据 = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()

	auth := map[string]string{"Authorization": "Bearer " + testClientKey}
	for _, bad := range []string{
		`not-json`,
		`{"hook_event_name":"PreToolUse","session_id":"s1"}`,
		`{"hook_event_name":"PreToolUse","session_id":"s1","tool_use_id":"t1","phase":"sideways"}`,
	} {
		resp := postJSON(t, srv.Client(), srv.URL+"/v1/tool-events?client=canonical", bad, auth)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("非法载荷 %s = %d, want 400", bad, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}
