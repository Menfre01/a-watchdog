package admin

import (
	"net/http"
	"testing"

	"ds-context/pkg/config/db"
	"ds-context/pkg/trace"
)

// TestChainSummary 覆盖会话链汇总:主链恒为 1 条,旁路按链去重计数,
// 缺 system 指纹时不给链数(退化的「1 条链」不是事实)。
func TestChainSummary(t *testing.T) {
	in := func(n int64) *int64 { return &n }
	cases := []struct {
		name       string
		steps      []db.ListSessionStepsRow
		lines      []trace.SnapshotLines
		sideSteps  map[int64]bool
		sideChains map[int64]int
		want       map[string]any
	}{
		{
			name: "只有主链",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, UsageIn: in(100)},
				{Seq: 2, UsageIn: in(200)},
			},
			lines:      []trace.SnapshotLines{{SystemFP: "sys-a"}, {SystemFP: "sys-a"}},
			sideSteps:  map[int64]bool{},
			sideChains: map[int64]int{},
			want: map[string]any{
				"main": 1, "side": 0, "side_steps": 0,
				"side_usage_in": int64(0), "side_usage_out": int64(0),
			},
		},
		{
			name: "含两条旁路链",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, UsageIn: in(100)},
				{Seq: 2, UsageIn: in(500), UsageOut: in(50)},
				{Seq: 3, UsageIn: in(600), UsageOut: in(60)},
				{Seq: 4, UsageIn: in(700)},
			},
			lines: []trace.SnapshotLines{
				{SystemFP: "sys-a"}, {SystemFP: "sys-b"}, {SystemFP: "sys-b"}, {SystemFP: "sys-c"},
			},
			sideSteps:  map[int64]bool{2: true, 3: true, 4: true},
			sideChains: map[int64]int{2: 1, 3: 1, 4: 2},
			want: map[string]any{
				"main": 1, "side": 2, "side_steps": 3,
				"side_usage_in": int64(1800), "side_usage_out": int64(110),
			},
		},
		{
			name: "缺 system 指纹退化",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, UsageIn: in(100)},
				{Seq: 2, UsageIn: in(100)},
			},
			lines:      []trace.SnapshotLines{{SystemFP: ""}, {SystemFP: ""}},
			sideSteps:  map[int64]bool{},
			sideChains: map[int64]int{},
			want:       map[string]any{"degraded": true, "note": "缺 system 指纹,链不可分"},
		},
		{
			name:       "单步会话不退化",
			steps:      []db.ListSessionStepsRow{{Seq: 1, UsageIn: in(100)}},
			lines:      []trace.SnapshotLines{{SystemFP: ""}},
			sideSteps:  map[int64]bool{},
			sideChains: map[int64]int{},
			want: map[string]any{
				"main": 1, "side": 0, "side_steps": 0,
				"side_usage_in": int64(0), "side_usage_out": int64(0),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			an := &sessionAnalysis{
				steps:       tc.steps,
				lines:       tc.lines,
				sideSteps:   tc.sideSteps,
				sideChainOf: tc.sideChains,
			}
			got := an.chainSummary()
			if len(got) != len(tc.want) {
				t.Fatalf("字段数 = %d(%v), want %d(%v)", len(got), got, len(tc.want), tc.want)
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s = %v(%T), want %v(%T)", key, got[key], got[key], want, want)
				}
			}
			// 退化时不得给链数:那是退化的「1 条链」,不是事实。
			if tc.want["degraded"] == true {
				for _, key := range []string{"main", "side", "side_steps", "side_usage_in", "side_usage_out"} {
					if _, has := got[key]; has {
						t.Errorf("退化时仍输出 %q: %v", key, got)
					}
				}
			}
		})
	}
}

// TestAdmin_TraceSkeletonChains 覆盖接线:骨架会话层给出链汇总,
// 而会话列表不含任何链字段(链归属要做整段指纹比对,成本随会话数放大)。
func TestAdmin_TraceSkeletonChains(t *testing.T) {
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
	writeStep(t, store, "2026-10-13T10:00:02Z", 200, 300, []trace.MainlineMessage{sideSys, ask}) // 旁路链:system 不同
	writeStep(t, store, "2026-10-13T10:00:05Z", 200, 1500, []trace.MainlineMessage{mainSys, task, assistantUse})

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID, "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)

	chains, ok := body["chains"].(map[string]any)
	if !ok {
		t.Fatalf("骨架缺 chains 字段: %+v", body["chains"])
	}
	if chains["main"] != float64(1) || chains["side"] != float64(1) {
		t.Errorf("链汇总 = %v, want main=1 side=1", chains)
	}
	if chains["side_steps"] != float64(1) {
		t.Errorf("旁路步数 = %v, want 1", chains["side_steps"])
	}
	if chains["side_usage_in"] != float64(100) {
		t.Errorf("旁路 in = %v, want 100", chains["side_usage_in"])
	}
	if _, has := chains["degraded"]; has {
		t.Errorf("有 system 指纹却标退化: %v", chains)
	}

	// 会话列表不含链字段:列表页不做整段指纹比对。
	listResp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces", "", cookie, false)
	var list []traceJSON
	decode(t, listResp, &list)
	if len(list) == 0 {
		t.Fatal("会话列表为空")
	}
	row := list[0]
	for key := range row {
		if key == "chains" || key == "chain" || key == "side_chains" {
			t.Errorf("会话列表出现链字段 %q: %v", key, row)
		}
	}
}
