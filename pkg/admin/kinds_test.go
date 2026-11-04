package admin

import (
	"net/http"
	"testing"

	"awatchdog/pkg/trace"
)

// TestAdmin_KindsNewEqualsChains 锁定不变量:分类为「首步」的步数等于链总数
// (主链 1 加旁路链 N)。它失败就说明分类或链归属出了问题,界面上那句说明也随之为假。
//
// 依据:pkg/trace 里每条链的第一步都由 firstDelta 判为 DeltaNew,Compare 从不产生
// 「首步」;唯一例外是链首快照为空(此时记成「无重合」),本测试用非空快照。
func TestAdmin_KindsNewEqualsChains(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mainSys := trace.MainlineMessage{Role: "system", Blocks: []trace.MainlineBlock{{"type": "text", "text": "你是主对话助手"}}}
	sideSys := trace.MainlineMessage{Role: "system", Blocks: []trace.MainlineBlock{{"type": "text", "text": "你是安全监控"}}}
	task := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": "帮我看看登录接口"}}}
	ask := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": "检查这一步"}}}
	assistantUse := trace.MainlineMessage{Role: "assistant", Blocks: []trace.MainlineBlock{
		{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"path": "a.go"}},
	}}

	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 1000, []trace.MainlineMessage{mainSys, task})
	writeStep(t, store, "2026-10-13T10:00:02Z", 200, 300, []trace.MainlineMessage{sideSys, ask}) // 旁路链首步
	writeStep(t, store, "2026-10-13T10:00:05Z", 200, 1500, []trace.MainlineMessage{mainSys, task, assistantUse})

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID, "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)

	kinds, _ := body["kinds"].(map[string]any)
	counts, _ := kinds["counts"].([]any)
	newSteps := -1.0
	for _, raw := range counts {
		row, _ := raw.(map[string]any)
		if row["kind"] == "new" {
			newSteps, _ = row["steps"].(float64)
		}
	}
	if newSteps < 0 {
		t.Fatalf("kinds 里找不到「首步」段: %v", counts)
	}

	chains, _ := body["chains"].(map[string]any)
	main, _ := chains["main"].(float64)
	side, _ := chains["side"].(float64)
	if newSteps != main+side {
		t.Errorf("「首步」步数 = %v, want 链总数 %v(主链 %v + 旁路 %v)", newSteps, main+side, main, side)
	}
	// 本用例必然含一条旁路链,否则这条不变量没有验证力。
	if side < 1 {
		t.Fatalf("用例未形成旁路链: %v", chains)
	}
}
