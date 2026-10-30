package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds-context/pkg/config/db"
	"ds-context/pkg/trace"
)

// upstreamOK 是返回固定成功响应的假上游。
func upstreamOK(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTraceEndToEnd 覆盖完整链路:HTTP 请求穿过责任链 → 异步落记录、原始层与快照 →
// 再从快照派生列重放推断(增量分类、轮次分组、工具配对)。
func TestTraceEndToEnd(t *testing.T) {
	store, handler := newTestStack(t, upstreamOK(t).URL)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	const sessionLabel = "e2e-session-1"
	sessionID := "1:" + sessionLabel
	headers := map[string]string{
		"Authorization":            "Bearer " + testClientKey,
		"x-claude-code-session-id": sessionLabel,
	}

	// 三轮请求,模拟真实的工具迭代形态:
	// 第 1 轮只有用户输入;第 2 轮把模型的工具调用带回;第 3 轮带回工具结果。
	step1 := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"帮我看看登录接口"}]}`
	step2 := `{"model":"claude-sonnet-4-5","messages":[
		{"role":"user","content":"帮我看看登录接口"},
		{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"path":"a.go"}}]}
	]}`
	step3 := `{"model":"claude-sonnet-4-5","messages":[
		{"role":"user","content":"帮我看看登录接口"},
		{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"path":"a.go"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file body","is_error":false}]}
	]}`
	for _, body := range []string{step1, step2, step3} {
		resp := postJSON(t, srv.Client(), srv.URL+"/v1/messages", body, headers)
		status := resp.StatusCode
		detail := ""
		if status != http.StatusOK {
			detail = readBody(t, resp)
		} else {
			_ = resp.Body.Close()
		}
		if status != http.StatusOK {
			t.Fatalf("上游请求状态 = %d, %s", status, detail)
		}
	}

	// 捕捉是异步的:等待三个快照落库。
	var steps []db.ListSessionStepsRow
	eventually(t, func() bool {
		rows, err := store.ListSessionSteps(context.Background(), sessionID)
		if err != nil {
			return false
		}
		steps = rows
		return len(rows) == 3
	})

	// 会话判定:显式标识 + client key 前缀。
	if steps[0].SessionSource != "explicit" {
		t.Errorf("判定来源 = %q, want explicit", steps[0].SessionSource)
	}
	if !strings.Contains(steps[0].SessionNote, "x-claude-code-session-id") {
		t.Errorf("判定理由 = %q", steps[0].SessionNote)
	}
	for i, step := range steps {
		if step.Seq != int64(i+1) {
			t.Errorf("第 %d 个快照序号 = %d, want %d", i, step.Seq, i+1)
		}
	}

	// 从落库的派生列重放推断:与视图层走同一条路径。
	lines := make([]trace.SnapshotLines, len(steps))
	for i, step := range steps {
		lines[i] = trace.SnapshotLinesOf(step.LineHashes, step.LineSizes)
	}
	deltas := trace.Analyze(lines)
	want := []struct {
		kind  trace.DeltaKind
		added int
	}{
		{trace.DeltaNew, 1},
		{trace.DeltaAdded, 1},
		{trace.DeltaAdded, 1},
	}
	for i, w := range want {
		if deltas[i].Kind != w.kind || deltas[i].Added != w.added {
			t.Errorf("第 %d 步分类 = %s/+%d, want %s/+%d",
				i+1, deltas[i].Kind, deltas[i].Added, w.kind, w.added)
		}
		if deltas[i].Removed != 0 {
			t.Errorf("第 %d 步移除行数 = %d, want 0(纯追加)", i+1, deltas[i].Removed)
		}
	}

	// 只解析新增区间,并据此配对工具调用。
	added := make([][]trace.MainlineMessage, len(steps))
	stepMsgs := make([]trace.StepMessages, len(steps))
	for i, step := range steps {
		ndjson, err := trace.Decompress(step.NdjsonGzip)
		if err != nil {
			t.Fatalf("解压第 %d 个快照失败: %v", i+1, err)
		}
		msgs, err := trace.ParseMessagesRange(ndjson, deltas[i].PrefixLen, deltas[i].CurLines)
		if err != nil {
			t.Fatalf("解析第 %d 个快照的新增区间失败: %v", i+1, err)
		}
		added[i] = msgs
		stepMsgs[i] = trace.StepMessages{Seq: step.Seq, Messages: msgs}
	}
	calls := trace.ExtractTools(stepMsgs)
	if len(calls) != 1 {
		t.Fatalf("工具调用数 = %d, want 1(%+v)", len(calls), calls)
	}
	got := calls[0]
	if got.Name != "Read" || got.Status != trace.ToolOK {
		t.Errorf("调用 = %+v, want Read/ok", got)
	}
	if got.IssuedSeq != 2 || got.ResultSeq != 3 {
		t.Errorf("发起/结果步 = %d/%d, want 2/3", got.IssuedSeq, got.ResultSeq)
	}

	// 轮次分组:工具迭代留在同一轮,不因 step 数而拆分。
	turnInputs := make([]trace.StepForTurn, len(steps))
	for i := range steps {
		turnInputs[i] = trace.StepForTurn{Seq: steps[i].Seq, Delta: deltas[i], Added: added[i]}
	}
	turns := trace.GroupTurns(turnInputs)
	if len(turns) != 1 || len(turns[0].StepSeqs) != 3 {
		t.Fatalf("轮次分组 = %+v, want 单轮三步", turns)
	}

	// 会话列表聚合:steps 即请求数,缓存 token 求和。
	summaries, err := store.ListSessionSummaries(context.Background(), "", "", "", 10)
	if err != nil || len(summaries) != 1 {
		t.Fatalf("会话列表 = %+v, %v", summaries, err)
	}
	if summaries[0].SessionID != sessionID || summaries[0].Steps != 3 {
		t.Errorf("列表行 = %+v", summaries[0])
	}
}

// TestTraceEndToEndSkipsAuxEndpoint 覆盖辅助端点:只落原始层与记录,不产快照。
func TestTraceEndToEndSkipsAuxEndpoint(t *testing.T) {
	store, handler := newTestStack(t, upstreamOK(t).URL)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	headers := map[string]string{
		"Authorization":            "Bearer " + testClientKey,
		"x-claude-code-session-id": "e2e-aux",
	}
	resp := postJSON(t, srv.Client(), srv.URL+"/v1/messages/count_tokens",
		`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count_tokens 状态 = %d %s", resp.StatusCode, readBody(t, resp))
	}
	_ = resp.Body.Close()

	// 等记录落库,再断言没有快照。
	eventually(t, func() bool {
		n, err := store.CountRequests(context.Background())
		return err == nil && n == 1
	})
	steps, err := store.ListSessionSteps(context.Background(), "1:e2e-aux")
	if err != nil || len(steps) != 0 {
		t.Fatalf("辅助端点不应产快照: %+v, %v", steps, err)
	}
}
