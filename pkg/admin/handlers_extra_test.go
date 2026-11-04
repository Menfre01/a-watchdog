package admin

import (
	"context"
	"net/http"
	"testing"
	"time"

	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
	"awatchdog/pkg/trace"
)

// TestMetricsWindow 覆盖统计窗口:1h / 7d / all 与默认 24h。
func TestMetricsWindow(t *testing.T) {
	if got := metricsWindow("all"); got != "" {
		t.Errorf("all = %q, want 空", got)
	}
	before := time.Now().UTC()
	cases := []struct {
		raw   string
		delta time.Duration
	}{
		{"1h", time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"24h", 24 * time.Hour},
		{"unknown", 24 * time.Hour},
	}
	for _, tc := range cases {
		raw := metricsWindow(tc.raw)
		got, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			t.Fatalf("metricsWindow(%q) = %q, 解析失败: %v", tc.raw, raw, err)
		}
		after := time.Now().UTC()
		if got.Before(before.Add(-tc.delta)) || got.After(after.Add(-tc.delta)) {
			t.Errorf("metricsWindow(%q) = %v, want 约 now-%v", tc.raw, got, tc.delta)
		}
	}
}

// TestPercentileClamp 覆盖分位数下溢与上溢时的夹取。
func TestPercentileClamp(t *testing.T) {
	if got := percentile([]int64{10, 20}, 0); got != 10 {
		t.Errorf("p=0 = %d, want 10(下溢取下界)", got)
	}
	if got := percentile([]int64{10, 20}, 1.5); got != 20 {
		t.Errorf("p=1.5 = %d, want 20(上溢取上界)", got)
	}
}

// TestAdmin_RequestMetricsBranches 覆盖空库与有用量时的比率计算。
func TestAdmin_RequestMetricsBranches(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	// 空库:计数、错误率、缓存命中率、分位数都为 0。
	var empty map[string]any
	decode(t, do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/metrics?window=all", "", cookie, false), &empty)
	if empty["count"] != float64(0) || empty["error_rate"] != float64(0) || empty["cache_rate"] != float64(0) {
		t.Errorf("空库指标 = %#v", empty)
	}
	if empty["p50_ms"] != float64(0) || empty["p95_ms"] != float64(0) || empty["p99_ms"] != float64(0) {
		t.Errorf("空库分位 = %#v", empty)
	}

	// 有数据:缓存命中率 = cache / (in + cache)。
	now := time.Now().UTC().Format(time.RFC3339Nano)
	in, out, cache := int64(100), int64(20), int64(50)
	reqs := []db.InsertRequestParams{
		{Ts: now, Protocol: "anthropic-messages", StatusCode: 200, LatencyMs: 100, Stream: 1,
			UsageIn: &in, UsageOut: &out, UsageCache: &cache},
		{Ts: now, Protocol: "anthropic-messages", StatusCode: 500, LatencyMs: 200, Stream: 1,
			UsageIn: &in, UsageOut: &out, UsageCache: &cache},
	}
	if _, err := store.InsertRequests(context.Background(), reqs); err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}
	var got map[string]any
	decode(t, do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/metrics?window=all", "", cookie, false), &got)
	if got["count"] != float64(2) || got["errors"] != float64(1) || got["error_rate"] != 0.5 {
		t.Errorf("计数 = %#v", got)
	}
	if got["cache_rate"] != 50.0/150.0 {
		t.Errorf("缓存命中率 = %v, want %v", got["cache_rate"], 50.0/150.0)
	}
	if got["usage_in"] != float64(200) || got["usage_cache"] != float64(100) {
		t.Errorf("用量 = %#v", got)
	}
}

// TestAdmin_TraceHandlerNotFound 覆盖 digest / step 的会话不存在分支。
func TestAdmin_TraceHandlerNotFound(t *testing.T) {
	_, _, srv := newTestAdmin(t)
	cookie := login(t, srv)
	for _, path := range []string{
		"/console/api/traces/1:none/digest",
		"/console/api/traces/1:none/1",
	} {
		resp := do(t, srv.Client(), http.MethodGet, srv.URL+path, "", cookie, false)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// TestAdmin_StoreErrorAllEndpoints 覆盖观测端点的存储错误分支(统一 500)。
func TestAdmin_StoreErrorAllEndpoints(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	paths := []string{
		"/console/api/traces",
		"/console/api/traces/1:x",
		"/console/api/traces/1:x/digest",
		"/console/api/traces/1:x/1",
		"/console/api/tool-events?session_id=1:x",
		"/console/api/prompt-events?session_id=1:x",
		"/console/api/metrics?window=all",
	}
	for _, path := range paths {
		resp := do(t, srv.Client(), http.MethodGet, srv.URL+path, "", cookie, false)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("GET %s = %d, want 500", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// TestAdmin_TraceCorruptSnapshot 覆盖快照损坏:该步按空处理,视图仍可读。
func TestAdmin_TraceCorruptSnapshot(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	ctx := context.Background()

	ids, err := store.InsertRequests(ctx, []db.InsertRequestParams{
		{Ts: "2026-10-13T10:00:00Z", Protocol: "anthropic-messages", StatusCode: 200, LatencyMs: 100, Stream: 1},
		{Ts: "2026-10-13T10:00:05Z", Protocol: "anthropic-messages", StatusCode: 200, LatencyMs: 100, Stream: 1},
	})
	if err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}
	for i, reqID := range ids {
		if err := store.SaveTrace(ctx, db.InsertRequestBodyParams{
			RequestID: reqID, Method: "POST", Path: "/v1/messages",
			BodyGzip: []byte("x"), Headers: `{}`,
		}, &config.TraceSnapshot{
			SessionID: testSessionID, SessionSource: "explicit", SessionNote: "n",
			Seq: int64(i + 1), Ts: "2026-10-13T10:00:0" + itoa(int64(i)) + "Z",
			NdjsonGzip: []byte("bad"), LineHashes: []byte{}, LineSizes: []byte{},
		}); err != nil {
			t.Fatalf("SaveTrace() error = %v", err)
		}
	}

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID, "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("骨架状态 = %d, want 200", resp.StatusCode)
	}
	var body traceJSON
	decode(t, resp, &body)
	if body["steps"] != float64(2) {
		t.Errorf("steps = %v, want 2", body["steps"])
	}
	// 两个快照都不可解:链归属退化,不输出链数。
	chains, _ := body["chains"].(map[string]any)
	if chains["degraded"] != true {
		t.Errorf("损坏快照应标退化: %#v", chains)
	}

	// 单步详情:新增区间为空数组,全量解析失败时回退。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/1?full=1", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("详情状态 = %d, want 200", resp.StatusCode)
	}
	var detail traceJSON
	decode(t, resp, &detail)
	msgs, ok := detail["messages"].([]any)
	if !ok || len(msgs) != 0 {
		t.Errorf("messages = %#v, want 空数组", detail["messages"])
	}
}

// TestSliceMessages 覆盖区间切分的越界收敛。
func TestSliceMessages(t *testing.T) {
	msgs := []trace.MainlineMessage{jsonTestMsg("a"), jsonTestMsg("b")}
	cases := []struct {
		name       string
		start, end int
		want       int
	}{
		{"正常", 0, 1, 1},
		{"起点为负", -3, 1, 1},
		{"终点越界", 0, 99, 2},
		{"终点为负", 0, -1, 2},
		{"空区间", 2, 2, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sliceMessages(msgs, tc.start, tc.end); len(got) != tc.want {
				t.Errorf("sliceMessages(%d,%d) = %d 条, want %d", tc.start, tc.end, len(got), tc.want)
			}
		})
	}
}

// TestAllMessagesOfBadGzip 覆盖整段快照解压失败。
func TestAllMessagesOfBadGzip(t *testing.T) {
	if _, err := allMessagesOf([]byte("bad")); err == nil {
		t.Fatal("损坏快照应返回错误")
	}
}

// TestAttributeToolsBranches 覆盖工具归属的未知步与链首步。
func TestAttributeToolsBranches(t *testing.T) {
	an := &sessionAnalysis{
		steps:     []db.ListSessionStepsRow{{Seq: 1}, {Seq: 2}},
		prevIdxOf: map[int]int{1: 0},
		tools: []trace.ToolCall{
			{ID: "unknown", IssuedSeq: 99},
			{ID: "chain-first", IssuedSeq: 1},
			{ID: "second", IssuedSeq: 2},
		},
	}
	an.attributeTools()
	if an.tools[0].IssuedSeq != 99 {
		t.Errorf("未知步不应改动: %d", an.tools[0].IssuedSeq)
	}
	if an.tools[1].IssuedSeq != 1 {
		t.Errorf("链首步不应改动: %d", an.tools[1].IssuedSeq)
	}
	if an.tools[2].IssuedSeq != 1 {
		t.Errorf("第二部应退到同链前驱 1: %d", an.tools[2].IssuedSeq)
	}
}
