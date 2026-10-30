package admin

import (
	"net/http"
	"testing"

	"ds-context/pkg/config/db"
	"ds-context/pkg/trace"
)

// TestHarnessBySeq 覆盖 harness 侧耗时:同链下一步的开始时间减去本步结束时间。
// 判据只走同链后继,故旁路请求的到达不会被算进主路步。
func TestHarnessBySeq(t *testing.T) {
	cases := []struct {
		name string
		// steps 按会话顺序给出;prevOf 是「同链上一步」的下标关系。
		steps  []db.ListSessionStepsRow
		prevOf map[int]int
		want   map[int64]int64
	}{
		{
			name: "相邻同链",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 1000},
				{Seq: 2, Ts: "2026-10-13T10:00:05Z", LatencyMs: 500},
			},
			prevOf: map[int]int{1: 0},
			// 本步结束 10:00:01 → 下一步开始 10:00:05 = 4000ms;末步无后继。
			want: map[int64]int64{1: 4000},
		},
		{
			name: "旁路插在中间",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 1000}, // 主链
				{Seq: 2, Ts: "2026-10-13T10:00:02Z", LatencyMs: 200},  // 旁路:不属于主链
				{Seq: 3, Ts: "2026-10-13T10:00:09Z", LatencyMs: 500},  // 主链
			},
			prevOf: map[int]int{2: 0}, // 下标 2 的同链上一步是下标 0
			// 主链:本步结束 10:00:01 → 10:00:09 = 8000ms(不含旁路的 10:00:02)。
			want: map[int64]int64{1: 8000},
		},
		{
			name: "负值不输出",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 9000},
				{Seq: 2, Ts: "2026-10-13T10:00:05Z", LatencyMs: 500},
			},
			prevOf: map[int]int{1: 0},
			// 后继早于本步结束:不可测,不输出(界面显示「—」,不是 0)。
			want: map[int64]int64{},
		},
		{
			name: "时间戳不可解析时不输出",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "不是时间", LatencyMs: 1000},
				{Seq: 2, Ts: "2026-10-13T10:00:05Z", LatencyMs: 500},
			},
			prevOf: map[int]int{1: 0},
			want:   map[int64]int64{},
		},
		{
			name: "单步的链没有后继",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 1000},
			},
			prevOf: map[int]int{},
			want:   map[int64]int64{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			an := &sessionAnalysis{steps: tc.steps, prevIdxOf: tc.prevOf}
			got := an.harnessBySeq()
			if len(got) != len(tc.want) {
				t.Fatalf("数量 = %d(%v), want %d(%v)", len(got), got, len(tc.want), tc.want)
			}
			for seq, want := range tc.want {
				if got[seq] != want {
					t.Errorf("seq=%d harness = %d, want %d", seq, got[seq], want)
				}
			}
		})
	}
}

// TestAdmin_TraceSkeletonHarness 覆盖接线:骨架每步给出 harness_ms,
// 链末步不输出该字段(界面显示「—」)。
func TestAdmin_TraceSkeletonHarness(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mainSys := trace.MainlineMessage{Role: "system", Blocks: []trace.MainlineBlock{{"type": "text", "text": "你是主对话助手"}}}
	task := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": "帮我看看登录接口"}}}
	assistantUse := trace.MainlineMessage{Role: "assistant", Blocks: []trace.MainlineBlock{
		{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"path": "a.go"}},
	}}
	userResult := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{
		{"type": "tool_result", "tool_use_id": "t1", "content": "file body", "is_error": false},
	}}

	// 三步同链:第一步结束 10:00:01,第二步开始 10:00:05 → 4000ms;
	// 第二步结束 10:00:07,第三步开始 10:00:12 → 5000ms;第三步是链末步。
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 1000, []trace.MainlineMessage{mainSys, task})
	writeStep(t, store, "2026-10-13T10:00:05Z", 200, 2000, []trace.MainlineMessage{mainSys, task, assistantUse})
	writeStep(t, store, "2026-10-13T10:00:12Z", 200, 500, []trace.MainlineMessage{mainSys, task, assistantUse, userResult})

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID, "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)

	steps := map[float64]map[string]any{}
	turns, _ := body["turns"].([]any)
	for _, traw := range turns {
		turn, _ := traw.(map[string]any)
		rawSteps, _ := turn["steps"].([]any)
		for _, raw := range rawSteps {
			s, _ := raw.(map[string]any)
			if seq, ok := s["seq"].(float64); ok {
				steps[seq] = s
			}
		}
	}
	if steps[1]["harness_ms"] != float64(4000) {
		t.Errorf("seq=1 harness_ms = %v, want 4000", steps[1]["harness_ms"])
	}
	if steps[2]["harness_ms"] != float64(5000) {
		t.Errorf("seq=2 harness_ms = %v, want 5000", steps[2]["harness_ms"])
	}
	// 链末步没有后继:不输出该字段,界面据此显示「—」。
	if _, has := steps[3]["harness_ms"]; has {
		t.Errorf("链末步不应输出 harness_ms: %+v", steps[3])
	}
}
