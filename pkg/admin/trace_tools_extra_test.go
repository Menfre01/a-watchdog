package admin

import (
	"context"
	"testing"
	"time"

	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
)

// TestUnionMillis 覆盖区间并集:重叠不双计,相邻合并,乱序先排序。
func TestUnionMillis(t *testing.T) {
	at := func(s string) time.Time {
		t.Helper()
		ts, err := time.Parse(time.RFC3339Nano, "2026-10-13T10:00:"+s+"Z")
		if err != nil {
			t.Fatalf("parse %s: %v", s, err)
		}
		return ts
	}
	cases := []struct {
		name  string
		items [][2]time.Time
		want  int64
		ok    bool
	}{
		{name: "空区间", items: nil, want: 0, ok: false},
		{name: "单个区间", items: [][2]time.Time{{at("02"), at("05")}}, want: 3000, ok: true},
		{name: "重叠区间", items: [][2]time.Time{{at("02"), at("05")}, {at("04"), at("06")}}, want: 4000, ok: true},
		{name: "包含区间", items: [][2]time.Time{{at("02"), at("06")}, {at("03"), at("04")}}, want: 4000, ok: true},
		{name: "相邻区间合并", items: [][2]time.Time{{at("02"), at("05")}, {at("05"), at("06")}}, want: 4000, ok: true},
		{name: "分离区间求和", items: [][2]time.Time{{at("07"), at("08")}, {at("02"), at("05")}}, want: 4000, ok: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := unionMillis(tc.items)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("unionMillis() = %d,%v want %d,%v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestAsTurnsAndAsSteps 覆盖骨架字段的两种切片形态与非法形态。
func TestAsTurnsAndAsSteps(t *testing.T) {
	direct := []map[string]any{{"index": 1}}
	if got := asTurns(map[string]any{"turns": direct}); len(got) != 1 || got[0]["index"] != 1 {
		t.Errorf("asTurns([]map) = %v", got)
	}
	mixed := []any{map[string]any{"index": 2}, "junk", nil}
	if got := asTurns(map[string]any{"turns": mixed}); len(got) != 1 || got[0]["index"] != 2 {
		t.Errorf("asTurns([]any) = %v", got)
	}
	if got := asTurns(map[string]any{"turns": "junk"}); got != nil {
		t.Errorf("asTurns(非法) = %v, want nil", got)
	}
	if got := asTurns(map[string]any{}); got != nil {
		t.Errorf("asTurns(缺失) = %v, want nil", got)
	}

	stepsDirect := []map[string]any{{"seq": int64(1)}}
	if got := asSteps(map[string]any{"steps": stepsDirect}); len(got) != 1 || got[0]["seq"] != int64(1) {
		t.Errorf("asSteps([]map) = %v", got)
	}
	stepsMixed := []any{map[string]any{"seq": int64(2)}, "junk"}
	if got := asSteps(map[string]any{"steps": stepsMixed}); len(got) != 1 || got[0]["seq"] != int64(2) {
		t.Errorf("asSteps([]any) = %v", got)
	}
	if got := asSteps(map[string]any{"steps": 42}); got != nil {
		t.Errorf("asSteps(非法) = %v, want nil", got)
	}
	if got := asSteps(map[string]any{}); got != nil {
		t.Errorf("asSteps(缺失) = %v, want nil", got)
	}
}

// insertToolEvent 写入一条工具事件(测试夹具)。
func insertToolEvent(t *testing.T, store *config.Store, callID, phase, ts string) {
	t.Helper()
	if err := store.InsertToolEvent(context.Background(), db.InsertToolEventParams{
		Client: "claude-code", SessionID: "1:tools", ToolCallID: callID,
		ToolName: "Bash", Phase: phase, Status: "ok", Boundary: "hook", Ts: ts,
	}); err != nil {
		t.Fatalf("InsertToolEvent() error = %v", err)
	}
}

// TestAttachToolDurations 覆盖工具事件归属:取区间并集,坏时间戳/未完成/早于首步的事件跳过。
func TestAttachToolDurations(t *testing.T) {
	a, store, _ := newTestAdmin(t)

	// 主路第 1 步结束于 10:00:01,第 2 步结束于 20:00:01。
	step1 := map[string]any{"seq": int64(1), "ts": "2026-10-13T10:00:00Z", "latency_ms": int64(1000)}
	step2 := map[string]any{"seq": int64(2), "ts": "2026-10-13T20:00:00Z", "latency_ms": int64(1000)}
	badSeq := map[string]any{"seq": "1", "ts": "2026-10-13T10:00:00Z", "latency_ms": int64(1)}
	badTs := map[string]any{"seq": int64(3), "ts": "不是时间", "latency_ms": int64(1)}
	skeleton := map[string]any{"turns": []any{
		map[string]any{"steps": []any{step1, badSeq, badTs}},
		map[string]any{"steps": []any{step2}},
	}}

	// 第 1 步:02–05、04–06 重叠,07–08 分离;并集 5s。
	insertToolEvent(t, store, "c1", "start", "2026-10-13T10:00:02Z")
	insertToolEvent(t, store, "c1", "end", "2026-10-13T10:00:05Z")
	insertToolEvent(t, store, "c2", "start", "2026-10-13T10:00:04Z")
	insertToolEvent(t, store, "c2", "end", "2026-10-13T10:00:06Z")
	insertToolEvent(t, store, "c3", "start", "2026-10-13T10:00:07Z")
	insertToolEvent(t, store, "c3", "end", "2026-10-13T10:00:08Z")
	// 未完成、坏开始、坏结束、零长度:全部跳过。
	insertToolEvent(t, store, "c4", "start", "2026-10-13T10:00:09Z")
	insertToolEvent(t, store, "c5", "start", "不是时间")
	insertToolEvent(t, store, "c5", "end", "2026-10-13T10:00:10Z")
	insertToolEvent(t, store, "c6", "start", "2026-10-13T10:00:10Z")
	insertToolEvent(t, store, "c6", "end", "不是时间")
	insertToolEvent(t, store, "c7", "start", "2026-10-13T10:00:10Z")
	insertToolEvent(t, store, "c7", "end", "2026-10-13T10:00:10Z")
	// 早于第一步结束 500ms 缓冲:idx<0,跳过。
	insertToolEvent(t, store, "c8", "start", "2026-10-13T09:00:00Z")
	insertToolEvent(t, store, "c8", "end", "2026-10-13T09:00:01Z")
	// 第 2 步:1s。
	insertToolEvent(t, store, "c9", "start", "2026-10-13T20:00:02Z")
	insertToolEvent(t, store, "c9", "end", "2026-10-13T20:00:03Z")

	a.attachToolDurations(context.Background(), &sessionAnalysis{sessionID: "1:tools"}, skeleton)

	if step1["tool_ms"] != int64(5000) {
		t.Errorf("第 1 步 tool_ms = %v, want 5000(重叠区间并集)", step1["tool_ms"])
	}
	if step2["tool_ms"] != int64(1000) {
		t.Errorf("第 2 步 tool_ms = %v, want 1000", step2["tool_ms"])
	}
	for _, step := range []map[string]any{badSeq, badTs} {
		if _, has := step["tool_ms"]; has {
			t.Errorf("无效步不应获得 tool_ms: %#v", step)
		}
	}
	// 没有工具事件的步不输出该字段:界面显示「—」。
	if _, has := step1["tool_ms_extra"]; has {
		t.Errorf("不应有额外字段: %#v", step1)
	}
}

// TestAttachToolDurationsNoEvents 覆盖空事件表与存储错误:静默返回,不修改骨架。
func TestAttachToolDurationsNoEvents(t *testing.T) {
	a, store, _ := newTestAdmin(t)
	skeleton := map[string]any{"turns": []any{map[string]any{"steps": []any{
		map[string]any{"seq": int64(1), "ts": "2026-10-13T10:00:00Z", "latency_ms": int64(1000)},
	}}}}
	a.attachToolDurations(context.Background(), &sessionAnalysis{sessionID: "1:empty"}, skeleton)
	steps := asSteps(asTurns(skeleton)[0])
	if _, has := steps[0]["tool_ms"]; has {
		t.Errorf("空事件表不应写入 tool_ms: %#v", steps[0])
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	a.attachToolDurations(context.Background(), &sessionAnalysis{sessionID: "1:tools"}, skeleton)
	if _, has := steps[0]["tool_ms"]; has {
		t.Errorf("存储错误不应写入 tool_ms: %#v", steps[0])
	}
}
