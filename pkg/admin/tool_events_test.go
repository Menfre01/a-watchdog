package admin

import (
	"context"
	"net/http"
	"testing"
	"time"

	"awatchdog/pkg/config/db"
)

// TestAdmin_ListToolEvents 覆盖读取接口:成对事件给出时长,缺结束的调用标记未完成。
func TestAdmin_ListToolEvents(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	ctx := context.Background()

	start := time.Now().UTC().Add(-2 * time.Second).Format(time.RFC3339Nano)
	end := time.Now().UTC().Format(time.RFC3339Nano)
	insert := func(callID, phase, ts, status string) {
		t.Helper()
		if err := store.InsertToolEvent(ctx, db.InsertToolEventParams{
			Client: "claude-code", SessionID: "1:s1", TurnID: "",
			ToolCallID: callID, ToolName: "Bash", Phase: phase,
			Status: status, Boundary: "hook", ApprovalWait: 0, Ts: ts,
		}); err != nil {
			t.Fatalf("InsertToolEvent() error = %v", err)
		}
	}
	insert("call_1", "start", start, "")
	insert("call_1", "end", end, "ok")
	insert("call_2", "start", start, "")

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/tool-events?session_id=1:s1", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取工具事件 = %d", resp.StatusCode)
	}
	var calls []map[string]any
	decode(t, resp, &calls)
	if len(calls) != 2 {
		t.Fatalf("调用数 = %d, want 2", len(calls))
	}
	if calls[0]["complete"] != true {
		t.Errorf("成对调用未标完成: %v", calls[0])
	}
	duration, ok := calls[0]["duration_ms"].(float64)
	if !ok || duration < 1900 {
		t.Errorf("时长 = %v, want 约 2000ms", calls[0]["duration_ms"])
	}
	if calls[1]["complete"] != false {
		t.Errorf("缺结束的调用不应标完成: %v", calls[1])
	}
	if _, has := calls[1]["duration_ms"]; has {
		t.Errorf("缺结束的调用不应给时长: %v", calls[1])
	}

	// 缺少 session_id → 400。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/tool-events", "", cookie, false)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺 session_id = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
