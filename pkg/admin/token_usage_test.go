package admin

import (
	"net/http"
	"testing"

	"awatchdog/pkg/trace"
)

// TestAdmin_TraceSkeletonProtocol 覆盖协议字段:用量列的口径随协议不同
// (Anthropic 的 in 不含缓存,OpenAI 兼容的 in 含缓存命中),视图据此标注。
func TestAdmin_TraceSkeletonProtocol(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	msgs := []trace.MainlineMessage{{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": "你好"}}}}
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 1000, msgs)

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID, "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)

	turns, _ := body["turns"].([]any)
	if len(turns) == 0 {
		t.Fatal("未取到轮次")
	}
	turn, _ := turns[0].(map[string]any)
	steps, _ := turn["steps"].([]any)
	if len(steps) == 0 {
		t.Fatal("未取到步")
	}
	step, _ := steps[0].(map[string]any)
	if step["protocol"] != "anthropic-messages" {
		t.Errorf("protocol = %v, want anthropic-messages", step["protocol"])
	}
}
