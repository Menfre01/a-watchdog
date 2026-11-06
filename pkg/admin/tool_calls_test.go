package admin

import (
	"context"
	"net/http"
	"testing"

	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
)

func insertToolCallEvent(t *testing.T, store *config.Store, callID, name, phase, ts, status string) {
	t.Helper()
	err := store.InsertToolEvent(context.Background(), db.InsertToolEventParams{
		Client: "claude-code", SessionID: testSessionID, ToolCallID: callID,
		ToolName: name, Phase: phase, Status: status, Boundary: "hook", Ts: ts,
	})
	if err != nil {
		t.Fatalf("InsertToolEvent() error = %v", err)
	}
}

func getToolCalls(t *testing.T, url string, cookie *http.Cookie) []map[string]any {
	t.Helper()
	resp := do(t, http.DefaultClient, http.MethodGet, url, "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取工具调用 = %d, want 200", resp.StatusCode)
	}
	var out []map[string]any
	decode(t, resp, &out)
	return out
}

// TestAdmin_ToolCallsMerge 覆盖事件与轨迹合并、只有轨迹、只有事件、
// 详情载荷与 step 归属。
func TestAdmin_ToolCallsMerge(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	seedTrajectory(t, store)

	insertToolCallEvent(t, store, "t1", "Read", "start", "2026-10-13T09:00:13Z", "")
	insertToolCallEvent(t, store, "t1", "Read", "end", "2026-10-13T09:00:16Z", "ok")
	// 只有事件、没有轨迹 step。
	insertToolCallEvent(t, store, "c-event-only", "mcp__fs__read", "start", "2026-10-13T09:00:20Z", "")
	insertToolCallEvent(t, store, "c-event-only", "mcp__fs__read", "end", "2026-10-13T09:00:21Z", "ok")

	calls := getToolCalls(t, srv.URL+"/console/api/traces/1:sess-a/tool-calls", cookie)
	if len(calls) != 2 {
		t.Fatalf("调用数 = %d, want 2: %#v", len(calls), calls)
	}
	byID := map[string]map[string]any{}
	for _, call := range calls {
		byID[call["tool_call_id"].(string)] = call
	}
	merged := byID["t1"]
	if merged == nil {
		t.Fatalf("缺少 t1: %#v", calls)
	}
	if merged["issued_seq"] == float64(0) || merged["result_seq"] == float64(0) {
		t.Errorf("t1 step 归属 = %v/%v", merged["issued_seq"], merged["result_seq"])
	}
	if merged["source"] != "local" || merged["status"] != "ok" {
		t.Errorf("t1 source/status = %v/%v", merged["source"], merged["status"])
	}
	if merged["duration_ms"] != float64(3000) {
		t.Errorf("t1 duration = %v, want 3000", merged["duration_ms"])
	}
	if merged["input_preview"] == "" || merged["input_preview"] == "—" {
		t.Errorf("t1 入参摘要 = %v", merged["input_preview"])
	}

	eventOnly := byID["c-event-only"]
	if eventOnly == nil {
		t.Fatalf("缺少只有事件的调用: %#v", calls)
	}
	if eventOnly["issued_seq"] != float64(0) || eventOnly["source"] != "mcp" {
		t.Errorf("事件调用 = %#v", eventOnly)
	}
	if eventOnly["duration_ms"] != float64(1000) {
		t.Errorf("事件调用 duration = %v, want 1000", eventOnly["duration_ms"])
	}

	var detail map[string]any
	resp := do(t, http.DefaultClient, http.MethodGet, srv.URL+"/console/api/traces/1:sess-a/tool-calls/t1", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取详情 = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &detail)
	if detail["input"] == nil || detail["result"] == nil {
		t.Errorf("详情载荷缺失: %#v", detail)
	}
	if detail["issued_seq"] == float64(0) {
		t.Errorf("详情缺少 step: %#v", detail)
	}
}

// TestAdmin_ToolCallsTraceOnly 覆盖轨迹有调用、没有事件的场景:
// 调用仍出现,状态取轨迹,耗时缺失。
func TestAdmin_ToolCallsTraceOnly(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	seedTrajectory(t, store)

	calls := getToolCalls(t, srv.URL+"/console/api/traces/1:sess-a/tool-calls", cookie)
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d, want 1: %#v", len(calls), calls)
	}
	call := calls[0]
	if call["tool_call_id"] != "t1" || call["status"] != "ok" {
		t.Errorf("轨迹调用 = %#v", call)
	}
	if _, has := call["duration_ms"]; has {
		t.Errorf("无事件调用不应有耗时: %#v", call)
	}
	if call["issued_seq"] == float64(0) {
		t.Errorf("轨迹调用缺少 step: %#v", call)
	}
}

// TestAdmin_ToolCallDetailNotFound 覆盖不存在的详情与缺少会话。
func TestAdmin_ToolCallDetailNotFound(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	seedTrajectory(t, store)
	resp := do(t, http.DefaultClient, http.MethodGet, srv.URL+"/console/api/traces/1:sess-a/tool-calls/nope", "", cookie, false)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("不存在的调用 = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
