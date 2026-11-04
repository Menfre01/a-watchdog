package admin

import (
	"net/http"
	"testing"

	"awatchdog/pkg/config/db"
	"awatchdog/pkg/trace"
)

// TestTurnAxis 覆盖时间轴偏移:轴跨度取本轮全部请求(含旁路步),
// 时间戳不可解析时退回不可用,不用零偏移冒充「同时开始」。
func TestTurnAxis(t *testing.T) {
	cases := []struct {
		name   string
		steps  []db.ListSessionStepsRow
		seqs   []int64
		want   map[int64]int64
		total  int64
		wantOK bool
	}{
		{
			name:   "单步轮",
			steps:  []db.ListSessionStepsRow{{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 1200}},
			seqs:   []int64{1},
			want:   map[int64]int64{1: 0},
			total:  1200,
			wantOK: true,
		},
		{
			name: "多步轮:偏移相对最早一步",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 1000},
				{Seq: 2, Ts: "2026-10-13T10:00:05Z", LatencyMs: 2000},
				{Seq: 3, Ts: "2026-10-13T10:00:20Z", LatencyMs: 500},
			},
			seqs:   []int64{1, 2, 3},
			want:   map[int64]int64{1: 0, 2: 5000, 3: 20000},
			total:  20500,
			wantOK: true,
		},
		{
			name: "含旁路步:旁路照常占轴",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 1000},
				{Seq: 2, Ts: "2026-10-13T10:00:02Z", LatencyMs: 400},
				{Seq: 3, Ts: "2026-10-13T10:00:10Z", LatencyMs: 500},
			},
			seqs:   []int64{1, 2, 3},
			want:   map[int64]int64{1: 0, 2: 2000, 3: 10000},
			total:  10500,
			wantOK: true,
		},
		{
			name: "起点不在第一步:取最早一步",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:10Z", LatencyMs: 1000},
				{Seq: 2, Ts: "2026-10-13T10:00:00Z", LatencyMs: 500},
			},
			seqs:   []int64{1, 2},
			want:   map[int64]int64{1: 10000, 2: 0},
			total:  11000,
			wantOK: true,
		},
		{
			name:   "时间戳不可解析:轴不可用",
			steps:  []db.ListSessionStepsRow{{Seq: 1, Ts: "不是时间", LatencyMs: 1000}},
			seqs:   []int64{1},
			want:   nil,
			total:  0,
			wantOK: false,
		},
		{
			name:   "空步骤表",
			steps:  nil,
			seqs:   nil,
			want:   nil,
			total:  0,
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			an := &sessionAnalysis{steps: tc.steps}
			offsets, total, ok := an.turnAxis(tc.seqs, indexSeq(tc.steps))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if total != tc.total {
				t.Errorf("总时长 = %d, want %d", total, tc.total)
			}
			for seq, want := range tc.want {
				if got := offsets[seq]; got != want {
					t.Errorf("seq=%d 偏移 = %d, want %d", seq, got, want)
				}
			}
			if !tc.wantOK && len(offsets) != 0 {
				t.Errorf("轴不可用时仍给出偏移: %v", offsets)
			}
		})
	}
}

// indexSeq 造「步序号 → 下标」表,与骨架里的 indexOf 同形。
func indexSeq(steps []db.ListSessionStepsRow) map[int64]int {
	out := make(map[int64]int, len(steps))
	for i, step := range steps {
		out[step.Seq] = i
	}
	return out
}

// TestTurnStory 覆盖一行叙述:段序固定为「首步分类 → 行数变化 → 首个细分标记 → 失败计数」,
// 旁路步不进叙述,不引入新语义。
func TestTurnStory(t *testing.T) {
	cases := []struct {
		name   string
		steps  []db.ListSessionStepsRow
		deltas []trace.Delta
		side   map[int64]bool
		want   []string // 每段文本;带 @序号 表示该段可跳转
	}{
		{
			name:  "首步轮",
			steps: []db.ListSessionStepsRow{{Seq: 1, Ts: "2026-10-13T10:00:00Z", StatusCode: 200}},
			deltas: []trace.Delta{
				{Kind: trace.DeltaNew, Added: 2},
			},
			want: []string{"会话首步@1", "新增 2 行"},
		},
		{
			name: "追加加失败",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:00Z", StatusCode: 200},
				{Seq: 2, Ts: "2026-10-13T10:00:05Z", StatusCode: 429},
			},
			deltas: []trace.Delta{
				{Kind: trace.DeltaNew, Added: 2},
				{Kind: trace.DeltaAdded, Added: 1},
			},
			want: []string{"会话首步@1", "新增 3 行", "1 步失败@2"},
		},
		{
			name: "压缩轮",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:00Z", StatusCode: 200},
				{Seq: 2, Ts: "2026-10-13T10:00:05Z", StatusCode: 200},
			},
			deltas: []trace.Delta{
				{Kind: trace.DeltaNew, Added: 10},
				{Kind: trace.DeltaRewrite, Added: 3, Removed: 8, Kept: 2, Summarized: true, PrevLines: 10, CurLines: 3},
			},
			// 行数合计含首步快照:10 + 3 = 13。
			want: []string{"会话首步@1", "移除 8 行 · 新增 13 行", "压缩 10→3 行@2"},
		},
		{
			name: "旁路步不进叙述",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:00Z", StatusCode: 200},
				{Seq: 2, Ts: "2026-10-13T10:00:02Z", StatusCode: 500},
				{Seq: 3, Ts: "2026-10-13T10:00:10Z", StatusCode: 200},
			},
			deltas: []trace.Delta{
				{Kind: trace.DeltaNew, Added: 2},
				{Kind: trace.DeltaChain, Added: 3},
				{Kind: trace.DeltaAdded, Added: 1},
			},
			side: map[int64]bool{2: true},
			want: []string{"会话首步@1", "新增 3 行"},
		},
		{
			name: "无行数变化",
			steps: []db.ListSessionStepsRow{
				{Seq: 1, Ts: "2026-10-13T10:00:00Z", StatusCode: 200},
			},
			deltas: []trace.Delta{
				{Kind: trace.DeltaRetry, RetryN: 1, RetryOf: 1},
			},
			want: []string{"重复@1", "无行数变化"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			an := &sessionAnalysis{steps: tc.steps, deltas: tc.deltas, sideSteps: tc.side}
			if an.sideSteps == nil {
				an.sideSteps = map[int64]bool{}
			}
			seqs := make([]int64, 0, len(tc.steps))
			for _, step := range tc.steps {
				seqs = append(seqs, step.Seq)
			}
			parts := an.turnStory(seqs, indexSeq(tc.steps))
			if len(parts) != len(tc.want) {
				t.Fatalf("段数 = %d(%v), want %d(%v)", len(parts), parts, len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				text := parts[i]["text"]
				if seq, ok := parts[i]["seq"]; ok {
					text = text.(string) + "@" + itoa(seq.(int64))
				}
				if text != want {
					t.Errorf("第 %d 段 = %v, want %v", i, text, want)
				}
			}
		})
	}
}

// itoa 把步序号写成字符串,便于在表里比较可跳转的段。
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// TestKindDistribution 覆盖分类分布:分项之和等于步数,细分单独计数、不并入分项。
func TestKindDistribution(t *testing.T) {
	an := &sessionAnalysis{deltas: []trace.Delta{
		{Kind: trace.DeltaNew},
		{Kind: trace.DeltaAdded},
		{Kind: trace.DeltaAdded},
		{Kind: trace.DeltaRetry},
		{Kind: trace.DeltaFork, TailUpdate: true},
		{Kind: trace.DeltaFork, TailUpdate: true},
		{Kind: trace.DeltaRewrite, Summarized: true},
		{Kind: trace.DeltaChain, Cleared: true},
	}}
	got := an.kindDistribution()

	counts, _ := got["counts"].([]map[string]any)
	if len(counts) != 6 {
		t.Fatalf("分类段数 = %d, want 6", len(counts))
	}
	sum := 0
	for _, row := range counts {
		steps, _ := row["steps"].(int)
		sum += steps
	}
	if sum != len(an.deltas) {
		t.Errorf("分项之和 = %d, want %d(步数)", sum, len(an.deltas))
	}
	if steps, ok := got["steps"].(int); !ok || steps != len(an.deltas) {
		t.Errorf("步数 = %v, want %d", got["steps"], len(an.deltas))
	}

	marks, _ := got["marks"].(map[string]any)
	want := map[string]int{"compressed": 1, "cleared": 1, "tail_update": 2}
	for name, n := range want {
		if marks[name] != n {
			t.Errorf("细分 %s = %v, want %d", name, marks[name], n)
		}
	}
	// 细分不计入分项:分项之和仍是步数,不是步数与细分之和。
	if sum != 8 {
		t.Errorf("分项之和 = %d, want 8(细分不并入)", sum)
	}
}

// TestAdmin_TraceSkeletonAxisAndKinds 覆盖骨架新增字段的接线:
// 每步给出 at_ms、轮次给出 duration_ms 与 story、会话层给出 kinds;
// 失败清单不新增字段,前端用 status_code 与 error_kind 自行派生。
func TestAdmin_TraceSkeletonAxisAndKinds(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	userTask := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": "帮我看看登录接口"}}}
	assistantUse := trace.MainlineMessage{Role: "assistant", Blocks: []trace.MainlineBlock{
		{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"path": "a.go"}},
	}}
	userResult := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{
		{"type": "tool_result", "tool_use_id": "t1", "content": "file body", "is_error": false},
	}}
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 1000, []trace.MainlineMessage{userTask})
	writeStep(t, store, "2026-10-13T10:00:05Z", 200, 2000, []trace.MainlineMessage{userTask, assistantUse})
	writeStep(t, store, "2026-10-13T10:00:20Z", 429, 500, []trace.MainlineMessage{userTask, assistantUse, userResult})

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
	wantOffsets := map[float64]float64{1: 0, 2: 5000, 3: 20000}
	for seq, want := range wantOffsets {
		if got := steps[seq]["at_ms"]; got != want {
			t.Errorf("seq=%v 的 at_ms = %v, want %v", seq, got, want)
		}
	}

	// 三步同属一轮:轴总时长 = 最后一步结束 − 最早一步开始。
	var firstTurn map[string]any
	if len(turns) > 0 {
		firstTurn, _ = turns[0].(map[string]any)
	}
	if firstTurn == nil {
		t.Fatal("未取到轮次")
	}
	if got := firstTurn["duration_ms"]; got != float64(20500) {
		t.Errorf("duration_ms = %v, want 20500", got)
	}
	story, _ := firstTurn["story"].([]any)
	if len(story) == 0 {
		t.Fatalf("story 为空: %+v", firstTurn)
	}
	last, _ := story[len(story)-1].(map[string]any)
	if last["text"] != "1 步失败" {
		t.Errorf("叙述末段 = %v, want 1 步失败", last)
	}

	// 会话层分类分布:分项之和等于步数。
	kinds, _ := body["kinds"].(map[string]any)
	counts, _ := kinds["counts"].([]any)
	if len(counts) != 6 {
		t.Fatalf("分类段数 = %d, want 6", len(counts))
	}
	sum := 0.0
	for _, raw := range counts {
		row, _ := raw.(map[string]any)
		n, _ := row["steps"].(float64)
		sum += n
	}
	if sum != body["steps"] {
		t.Errorf("分项之和 = %v, want %v", sum, body["steps"])
	}

	// 失败步的派生输入必须在骨架里:前端据此列失败清单,不新增字段。
	failed := steps[3]
	if failed["status_code"] != float64(429) {
		t.Errorf("失败步缺状态码: %+v", failed)
	}
	if _, has := failed["failed"]; has {
		t.Errorf("步骨架不应输出失败标记字段: %+v", failed)
	}
	for _, key := range []string{"failures_detail", "failure_list"} {
		if _, has := firstTurn[key]; has {
			t.Errorf("轮次不应输出失败列表字段 %q", key)
		}
	}
}
