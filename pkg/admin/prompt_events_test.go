package admin

import (
	"context"
	"net/http"
	"testing"
	"time"

	"awatchdog/pkg/config/db"
)

// TestAdmin_ListPromptEvents 覆盖提示提交读取接口。
func TestAdmin_ListPromptEvents(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.InsertPromptEvent(context.Background(), db.InsertPromptEventParams{
		Client: "claude-code", SessionID: "1:p1", TurnID: "",
		PromptID: "pr1", PromptLen: 6, Ts: ts,
	}); err != nil {
		t.Fatalf("InsertPromptEvent() error = %v", err)
	}

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/prompt-events?session_id=1:p1", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取 = %d, want 200", resp.StatusCode)
	}
	var rows []map[string]any
	decode(t, resp, &rows)
	if len(rows) != 1 || rows[0]["prompt_len"] != float64(6) {
		t.Fatalf("提交事件 = %v", rows)
	}

	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/prompt-events", "", cookie, false)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺参数 = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
