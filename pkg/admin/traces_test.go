package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"ds-context/pkg/config"
	"ds-context/pkg/config/db"
	"ds-context/pkg/trace"
)

// testSessionID 是测试会话的标识(key_id=1 的前缀形式)。
const testSessionID = "1:sess-a"

// writeStep 写入一条请求记录与对应快照(测试夹具)。
func writeStep(t *testing.T, store *config.Store, ts string, status, latency int64, msgs []trace.MainlineMessage) {
	t.Helper()
	writeStepOpt(t, store, ts, status, latency, msgs, true)
}

// writeStepOpt 与 writeStep 相同,但可关掉行长度列:withSizes 为 false 时
// 落空 blob(列声明为 NOT NULL,不能落 NULL),模拟只落指纹、长度不可用的数据,
// 用于覆盖长度不可比的退化路径。
func writeStepOpt(t *testing.T, store *config.Store, ts string, status, latency int64,
	msgs []trace.MainlineMessage, withSizes bool) {
	t.Helper()
	ctx := context.Background()
	usage := int64(100)
	ids, err := store.InsertRequests(ctx, []db.InsertRequestParams{{
		Ts:             ts,
		Protocol:       "anthropic-messages",
		RequestedModel: "claude-sonnet-4-5",
		StatusCode:     status,
		LatencyMs:      latency,
		Stream:         1,
		UsageIn:        &usage,
	}})
	if err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}
	serialized, err := trace.Serialize(msgs)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	compressed, err := trace.Compress(serialized.NDJSON)
	if err != nil {
		t.Fatalf("Compress() error = %v", err)
	}
	seq, err := store.NextSnapshotSeq(ctx, testSessionID)
	if err != nil {
		t.Fatalf("NextSnapshotSeq() error = %v", err)
	}
	lineSizes := []byte{}
	if withSizes {
		lineSizes = serialized.LineSizes
	}
	err = store.SaveTrace(ctx, db.InsertRequestBodyParams{
		RequestID: ids[0], Method: "POST", Path: "/v1/messages",
		BodyGzip: compressed, Headers: `{"x-claude-code-session-id":["sess-a"]}`,
	}, &config.TraceSnapshot{
		SessionID: testSessionID, SessionSource: "explicit",
		SessionNote: "explicit: x-claude-code-session-id",
		Seq:         seq, Ts: ts, NdjsonGzip: compressed,
		LineHashes: serialized.LineHashes, LineSizes: lineSizes,
	})
	if err != nil {
		t.Fatalf("SaveTrace() error = %v", err)
	}
}

// seedTrajectory 造一个含工具迭代、重试与新输入的会话。
func seedTrajectory(t *testing.T, store *config.Store) {
	t.Helper()
	userTask := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": "帮我看看登录接口"}}}
	assistantUse := trace.MainlineMessage{Role: "assistant", Blocks: []trace.MainlineBlock{
		{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"path": "a.go"}},
	}}
	userResult := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{
		{"type": "tool_result", "tool_use_id": "t1", "content": "file body", "is_error": false},
	}}

	// 真实形态:模型的工具调用在下一轮请求里带回,工具结果再下一轮带回。
	step1 := []trace.MainlineMessage{userTask}
	step2 := []trace.MainlineMessage{userTask, assistantUse}
	step3 := []trace.MainlineMessage{userTask, assistantUse, userResult}
	writeStep(t, store, "2026-10-13T09:00:00Z", 200, 2400, step1)
	writeStep(t, store, "2026-10-13T09:00:12Z", 200, 3100, step2)
	writeStep(t, store, "2026-10-13T09:00:15Z", 429, 300, step3)  // 首次尝试:失败
	writeStep(t, store, "2026-10-13T09:00:18Z", 429, 300, step3)  // 重试
	writeStep(t, store, "2026-10-13T09:00:21Z", 200, 2900, step3) // 重试成功
}

// traceJSON 是解码响应用的宽松容器。
type traceJSON = map[string]any

// TestAdmin_ListTraces 覆盖会话列表与过滤。
func TestAdmin_ListTraces(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	seedTrajectory(t, store)

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("列表状态 = %d", resp.StatusCode)
	}
	var list []traceJSON
	decode(t, resp, &list)
	if len(list) != 1 {
		t.Fatalf("会话数 = %d, want 1", len(list))
	}
	got := list[0]
	if got["session_id"] != testSessionID || got["steps"] != float64(5) {
		t.Errorf("列表行 = %v", got)
	}
	if got["session_source"] != "explicit" {
		t.Errorf("判定来源 = %v", got["session_source"])
	}
	// agent 分类由判定理由派生:测试夹具用 claude 头名。
	if got["agent"] != "claude-code" {
		t.Errorf("agent = %v, want claude-code", got["agent"])
	}
	// 列表不含轮次数(推断成本随会话数放大)。
	if _, exists := got["turns"]; exists {
		t.Errorf("列表不应含轮次数: %v", got)
	}

	// 前缀过滤:不匹配时为空。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces?q=nope", "", cookie, false)
	var none []traceJSON
	decode(t, resp, &none)
	if len(none) != 0 {
		t.Errorf("前缀过滤 = %v, want 空", none)
	}
	// client key 过滤。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces?key_id=1", "", cookie, false)
	var byKey []traceJSON
	decode(t, resp, &byKey)
	if len(byKey) != 1 {
		t.Errorf("key_id 过滤 = %v", byKey)
	}
	// 密钥与标识前缀叠加:两者都匹配才入选。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces?key_id=1&q=sess", "", cookie, false)
	var combined []traceJSON
	decode(t, resp, &combined)
	if len(combined) != 1 {
		t.Errorf("叠加过滤 = %v, want 1 条", combined)
	}
	// 前缀不匹配时为空,说明 q 没有被 key_id 覆盖。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces?key_id=1&q=nope", "", cookie, false)
	decode(t, resp, &combined)
	if len(combined) != 0 {
		t.Errorf("叠加过滤(不匹配) = %v, want 空", combined)
	}
	// 其他密钥下没有该会话。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces?key_id=2&q=sess", "", cookie, false)
	decode(t, resp, &combined)
	if len(combined) != 0 {
		t.Errorf("叠加过滤(其他密钥) = %v, want 空", combined)
	}
	// 只给前缀、不给密钥时,按客户端原始标识匹配:界面显示什么就能搜什么。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces?q=sess", "", cookie, false)
	decode(t, resp, &combined)
	if len(combined) != 1 {
		t.Errorf("原始标识前缀过滤 = %v, want 1 条", combined)
	}
	// 存储前缀(密钥 id 加冒号)不是显示值,不应命中。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces?q=1:", "", cookie, false)
	decode(t, resp, &combined)
	if len(combined) != 0 {
		t.Errorf("存储前缀过滤 = %v, want 空", combined)
	}
}

// TestAdmin_TraceDetail 覆盖轨迹骨架:轮次、步、增量分类与重试组。
func TestAdmin_TraceDetail(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	seedTrajectory(t, store)

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID, "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("骨架状态 = %d", resp.StatusCode)
	}
	var body traceJSON
	decode(t, resp, &body)
	if body["steps"] != float64(5) {
		t.Errorf("steps = %v", body["steps"])
	}
	turns, ok := body["turns"].([]any)
	if !ok || len(turns) == 0 {
		t.Fatalf("turns = %v", body["turns"])
	}
	first, _ := turns[0].(map[string]any)
	steps, _ := first["steps"].([]any)
	if len(steps) != 5 {
		t.Fatalf("首轮 step 数 = %d, want 5(工具迭代与重试留在同轮)", len(steps))
	}
	// 首步分类为 new,并带判定理由。
	step0, _ := steps[0].(map[string]any)
	delta0, _ := step0["delta"].(map[string]any)
	if delta0["kind"] != "new" || delta0["note"] == "" {
		t.Errorf("首步 delta = %v", delta0)
	}
	// 第 2 步为追加(+1:模型的工具调用被下一轮请求带回)。
	step1, _ := steps[1].(map[string]any)
	delta1, _ := step1["delta"].(map[string]any)
	if delta1["kind"] != "added" || delta1["added"] != float64(1) {
		t.Errorf("第 2 步 delta = %v", delta1)
	}
	// 重试组:第 3 步为首次尝试,第 4、5 步为重试(共 3 次)。
	step2, _ := steps[2].(map[string]any)
	retry2, _ := step2["retry"].(map[string]any)
	if retry2["of"] != float64(3) {
		t.Errorf("重试组 = %v", retry2)
	}
	// 失败标记:第 3、4 步是 429。
	if first["failures"] != float64(2) {
		t.Errorf("失败步数 = %v, want 2", first["failures"])
	}
}

// TestAdmin_TraceDigest 覆盖渐进填充:轮次标题与工具聚合。
func TestAdmin_TraceDigest(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	seedTrajectory(t, store)

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/digest", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("digest 状态 = %d", resp.StatusCode)
	}
	var body traceJSON
	decode(t, resp, &body)

	turns, _ := body["by_turn"].([]any)
	if len(turns) == 0 {
		t.Fatalf("by_turn = %v", body["by_turn"])
	}
	first, _ := turns[0].(map[string]any)
	if first["title"] != "帮我看看登录接口" {
		t.Errorf("轮次标题 = %v", first["title"])
	}

	totals, _ := body["totals"].(map[string]any)
	if totals["calls"] != float64(1) || totals["ok"] != float64(1) {
		t.Errorf("totals = %v", totals)
	}
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("工具聚合 = %v", tools)
	}
	// step 行据此显示工具标签:工具归到生成该调用的步(step 1),不归到首次出现的 step 2。
	byStep, _ := body["by_step"].([]any)
	if len(byStep) != 1 {
		t.Fatalf("by_step = %v, want 1 条(仅 step 1 发起调用)", byStep)
	}
	stepRow, _ := byStep[0].(map[string]any)
	if stepRow["seq"] != float64(1) || stepRow["calls"] != float64(1) {
		t.Errorf("step 工具分布 = %v", stepRow)
	}
	if names, _ := stepRow["by_name"].(map[string]any); names["Read"] != float64(1) {
		t.Errorf("step 工具名分布 = %v", stepRow["by_name"])
	}
	tool, _ := tools[0].(map[string]any)
	if tool["name"] != "Read" || tool["source"] != "内置" {
		t.Errorf("工具行 = %v", tool)
	}
	if gaps, ok := tool["gaps_ms"].([]any); !ok || len(gaps) != 1 {
		t.Errorf("间隔 = %v", tool["gaps_ms"])
	}
}

// TestAdmin_TraceStep 覆盖单步详情与 full 切换。
func TestAdmin_TraceStep(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	seedTrajectory(t, store)

	// 默认:只返回新增消息(第 2 步新增的是模型的工具调用)。
	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("step 状态 = %d", resp.StatusCode)
	}
	var body traceJSON
	decode(t, resp, &body)
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 1 {
		t.Errorf("新增消息数 = %d, want 1", len(msgs))
	}
	if body["inherited"] != float64(1) {
		t.Errorf("继承行数 = %v, want 1", body["inherited"])
	}
	// 工具归到生成它的 step 1,step 2 的明细为空。
	if tools, _ := body["tools"].([]any); len(tools) != 0 {
		t.Errorf("step 2 工具明细 = %v, want 空", tools)
	}

	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/1", "", cookie, false)
	var firstBody traceJSON
	decode(t, resp, &firstBody)
	firstTools, _ := firstBody["tools"].([]any)
	if len(firstTools) != 1 {
		t.Fatalf("step 1 工具明细 = %v", firstTools)
	}
	firstTool, _ := firstTools[0].(map[string]any)
	if firstTool["name"] != "Read" || firstTool["status"] != "ok" {
		t.Errorf("step 1 工具明细 = %v", firstTool)
	}

	// full=1:返回全量消息(用户输入 + 工具调用)。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2?full=1", "", cookie, false)
	var fullBody traceJSON
	decode(t, resp, &fullBody)
	fullMsgs, _ := fullBody["messages"].([]any)
	if len(fullMsgs) != 2 {
		t.Errorf("全量消息数 = %d, want 2", len(fullMsgs))
	}

	// 不存在的 step。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/99", "", cookie, false)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("未知 step 状态 = %d, want 404", resp.StatusCode)
	}
	// 非法 step 序号。
	resp = do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/abc", "", cookie, false)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("非法 seq 状态 = %d, want 400", resp.StatusCode)
	}
}

// TestAdmin_TraceStepRetryMessagesIsArray 覆盖重试步的空新增区间:
// messages 必须是空数组而不是 null——前端会对它取 length,null 会让界面崩溃。
func TestAdmin_TraceStepRetryMessagesIsArray(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	seedTrajectory(t, store)

	// seq=4 与 seq=3 的快照完全相同,是重试步:相对上一步没有新增行。
	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/4", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态 = %d", resp.StatusCode)
	}
	var body traceJSON
	decode(t, resp, &body)
	msgs, ok := body["messages"].([]any)
	if !ok {
		t.Fatalf("messages = %#v, want 数组(不能是 null)", body["messages"])
	}
	if len(msgs) != 0 {
		t.Errorf("重试步 messages = %d 条, want 0(新增区间为空)", len(msgs))
	}
	if body["inherited"] != float64(3) {
		t.Errorf("继承行数 = %v, want 3", body["inherited"])
	}
}

// TestAdmin_TraceRequiresSession 覆盖鉴权:未登录不得读取轨迹。
func TestAdmin_TraceRequiresSession(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	seedTrajectory(t, store)

	for _, path := range []string{
		"/console/api/traces",
		"/console/api/traces/" + testSessionID,
		"/console/api/traces/" + testSessionID + "/digest",
		"/console/api/traces/" + testSessionID + "/1",
	} {
		resp := do(t, srv.Client(), http.MethodGet, srv.URL+path, "", nil, false)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s 未登录状态 = %d, want 401", path, resp.StatusCode)
		}
	}
}

// TestAdmin_TraceSessionNotFound 覆盖不存在的会话。
func TestAdmin_TraceSessionNotFound(t *testing.T) {
	_, _, srv := newTestAdmin(t)
	cookie := login(t, srv)

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/1:none", "", cookie, false)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("未知会话状态 = %d, want 404", resp.StatusCode)
	}
}

// TestParseLimit 覆盖条数上限解析。
func TestParseLimit(t *testing.T) {
	cases := map[string]int64{
		"":      traceListDefaultLimit,
		"abc":   traceListDefaultLimit,
		"0":     traceListDefaultLimit,
		"-5":    traceListDefaultLimit,
		"50":    50,
		"99999": traceListMaxLimit,
	}
	for raw, want := range cases {
		if got := parseLimit(raw); got != want {
			t.Errorf("parseLimit(%q) = %d, want %d", raw, got, want)
		}
	}
}

// TestGapMs 覆盖间隔计算与负值归零。
func TestGapMs(t *testing.T) {
	prev := db.ListSessionStepsRow{Ts: "2026-10-13T09:00:00Z", LatencyMs: 2000}
	cur := db.ListSessionStepsRow{Ts: "2026-10-13T09:00:10Z"}
	if got := gapMs(prev, cur); got != 8000 {
		t.Errorf("gapMs = %d, want 8000", got)
	}
	// 时间戳倒挂或非法时归零,不产生负值。
	bad := db.ListSessionStepsRow{Ts: "not-a-time"}
	if got := gapMs(prev, bad); got != 0 {
		t.Errorf("非法时间戳 = %d, want 0", got)
	}
}

// TestTurnTitleAndTruncate 覆盖标题提取与截断。
func TestTurnTitleAndTruncate(t *testing.T) {
	long := make([]byte, 0, 200)
	for range 200 {
		long = append(long, 'a')
	}
	msgs := []trace.MainlineMessage{
		{Role: "assistant", Blocks: []trace.MainlineBlock{{"type": "text", "text": "忽略"}}},
		{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": string(long)}}},
	}
	got := turnTitle(msgs)
	if len([]rune(got)) != turnTitleMaxRunes+1 {
		t.Errorf("截断后字符数 = %d, want %d", len([]rune(got)), turnTitleMaxRunes+1)
	}
	if turnTitle(nil) != "" {
		t.Error("空输入应返回空标题")
	}
}

// TestToolSourceAndFailed 覆盖来源判定与失败判定。
func TestToolSourceAndFailed(t *testing.T) {
	if toolSource("mcp__github__create_issue") != "MCP" || toolSource("Read") != "内置" {
		t.Error("来源判定不符")
	}
	kind := "auth"
	if !failed(db.ListSessionStepsRow{StatusCode: 401}) {
		t.Error("401 应判定为失败")
	}
	if !failed(db.ListSessionStepsRow{StatusCode: 200, ErrorKind: &kind}) {
		t.Error("带归因应判定为失败")
	}
	if failed(db.ListSessionStepsRow{StatusCode: 200}) {
		t.Error("正常响应不应判定为失败")
	}
}

// TestTraceJSONMarshalable 覆盖响应可序列化(无循环或不可编码值)。
func TestTraceJSONMarshalable(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	seedTrajectory(t, store)

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID, "", cookie, false)
	var raw json.RawMessage
	decode(t, resp, &raw)
	if len(raw) == 0 {
		t.Fatal("空响应体")
	}
}

// TestAdmin_TraceStepDiffHunks 覆盖差异块:中间若干消息被改动时,
// 只报真正变动的部分,且给出两侧的行号范围与内容。
func TestAdmin_TraceStepDiffHunks(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mk := func(texts ...string) []trace.MainlineMessage {
		out := make([]trace.MainlineMessage, 0, len(texts))
		for _, s := range texts {
			out = append(out, trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": s}}})
		}
		return out
	}
	// 前缀 2 条相同;中间 2 条被替换为 3 条。
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 100, mk("common1", "common2", "old-a", "old-b"))
	writeStep(t, store, "2026-10-13T10:00:10Z", 200, 100, mk("common1", "common2", "new-x", "new-y", "new-z"))

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态 = %d", resp.StatusCode)
	}
	var body traceJSON
	decode(t, resp, &body)

	hunks, ok := body["diff"].([]any)
	if !ok || len(hunks) != 1 {
		t.Fatalf("diff = %#v, want 单块", body["diff"])
	}
	h, _ := hunks[0].(map[string]any)
	if h["kind"] != "replace" {
		t.Errorf("类型 = %v, want replace", h["kind"])
	}
	// 两侧的行号范围:旧侧第 3–4 行(0 基 2 起 2 行),新侧第 3–5 行。
	if h["prev_start"] != float64(2) || h["prev_count"] != float64(2) {
		t.Errorf("旧侧范围 = %v+%v, want 2+2", h["prev_start"], h["prev_count"])
	}
	if h["cur_start"] != float64(2) || h["cur_count"] != float64(3) {
		t.Errorf("新侧范围 = %v+%v, want 2+3", h["cur_start"], h["cur_count"])
	}
	prevRows, _ := h["prev"].([]any)
	curRows, _ := h["cur"].([]any)
	if len(prevRows) != 2 || len(curRows) != 3 {
		t.Fatalf("两侧行数 = %d/%d, want 2/3", len(prevRows), len(curRows))
	}
	// 每行带完整上下文中的行号与消息内容。
	p0, _ := prevRows[0].(map[string]any)
	if p0["line"] != float64(2) || p0["message"] == nil {
		t.Errorf("旧侧首行 = %v", p0)
	}
	if _, has := p0["bytes"]; !has {
		t.Errorf("旧侧首行缺字节数: %v", p0)
	}
	if body["diff_lines"] != float64(3) {
		t.Errorf("diff_lines = %v, want 3(取两侧较长者)", body["diff_lines"])
	}
}

// TestAdmin_TraceStepDiffDeletion 覆盖消息被删除:旧侧有内容、新侧为空。
// 这是逐位置对齐表达不了的形态,必须靠 LCS 差异块。
func TestAdmin_TraceStepDiffDeletion(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mk := func(texts ...string) []trace.MainlineMessage {
		out := make([]trace.MainlineMessage, 0, len(texts))
		for _, s := range texts {
			out = append(out, trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": s}}})
		}
		return out
	}
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 100, mk("a", "会被删掉的", "b", "c"))
	writeStep(t, store, "2026-10-13T10:00:10Z", 200, 100, mk("a", "b", "c"))

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2", "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)

	hunks, _ := body["diff"].([]any)
	if len(hunks) != 1 {
		t.Fatalf("diff = %#v, want 单块", body["diff"])
	}
	h, _ := hunks[0].(map[string]any)
	if h["kind"] != "delete" {
		t.Errorf("类型 = %v, want delete", h["kind"])
	}
	if h["prev_count"] != float64(1) || h["cur_count"] != float64(0) {
		t.Errorf("范围 = 旧 %v / 新 %v, want 1 / 0", h["prev_count"], h["cur_count"])
	}
	prevRows, _ := h["prev"].([]any)
	if len(prevRows) != 1 {
		t.Fatalf("旧侧应有 1 行,实际 %d", len(prevRows))
	}
	row, _ := prevRows[0].(map[string]any)
	if row["line"] != float64(1) {
		t.Errorf("被删除行的位置 = %v, want 1", row["line"])
	}
	// 被删除的一侧没有内容 → 不输出 cur 字段。
	if _, has := h["cur"]; has {
		t.Errorf("删除块不应有新侧内容: %v", h["cur"])
	}
}

// TestAdmin_TraceSkeletonDivergedLines 覆盖骨架层的差异行数:
// 徽标据此显示「分叉 N 行」,无需加载两侧内容。
func TestAdmin_TraceSkeletonDivergedLines(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mk := func(texts ...string) []trace.MainlineMessage {
		out := make([]trace.MainlineMessage, 0, len(texts))
		for _, s := range texts {
			out = append(out, trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": s}}})
		}
		return out
	}
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 100, mk("a", "b", "c"))
	writeStep(t, store, "2026-10-13T10:00:10Z", 200, 100, mk("a", "b", "X"))

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID, "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)
	turns, _ := body["turns"].([]any)
	// 第二步含人类输入 → 自成一个轮次,故要扫全部轮次。
	var checked bool
	for _, traw := range turns {
		turn, _ := traw.(map[string]any)
		steps, _ := turn["steps"].([]any)
		for _, raw := range steps {
			s, _ := raw.(map[string]any)
			if s["seq"] != float64(2) {
				continue
			}
			checked = true
			if s["diff_lines"] != float64(1) {
				t.Errorf("diff_lines = %v, want 1", s["diff_lines"])
			}
			delta, _ := s["delta"].(map[string]any)
			if delta["kind"] != "fork" {
				t.Errorf("分类 = %v, want fork", delta["kind"])
			}
		}
	}
	if !checked {
		t.Fatal("未找到 seq=2")
	}
}

// TestAdmin_TraceSkeletonTailUpdate 覆盖末行更新的骨架字段:
// 方向由 tail_before / tail_after 给出,界面据此显示扩展或收缩;
// 行长度缺失时只给 tail_update,方向交给判定理由说明不可比。
func TestAdmin_TraceSkeletonTailUpdate(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mk := func(texts ...string) []trace.MainlineMessage {
		out := make([]trace.MainlineMessage, 0, len(texts))
		for _, s := range texts {
			out = append(out, trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": s}}})
		}
		return out
	}
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 100, mk("sys", "固定", "转录-短"))
	writeStep(t, store, "2026-10-13T10:00:10Z", 200, 100, mk("sys", "固定", "转录-变长了很多很多内容"))
	writeStep(t, store, "2026-10-13T10:00:20Z", 200, 100, mk("sys", "固定", "转录-短"))
	// 第 4 步只落指纹:长度列缺失,方向不可判。
	writeStepOpt(t, store, "2026-10-13T10:00:30Z", 200, 100, mk("sys", "固定", "转录-中"), false)

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
	// 每步都是含人类输入的新轮次,故 4 步分列 4 个轮次,按 seq 取值即可。
	for _, seq := range []float64{2, 3, 4} {
		s, ok := steps[seq]
		if !ok {
			t.Fatalf("未找到 seq=%v(已收集 %d 步)", seq, len(steps))
		}
		if s["tail_update"] != true {
			t.Fatalf("seq=%v 未标记 tail_update: %+v", seq, s)
		}
	}

	grow, shrink := steps[2], steps[3]
	if before, after := grow["tail_before"].(float64), grow["tail_after"].(float64); after <= before {
		t.Errorf("变长方向的字节数 = %v → %v, want 递增", before, after)
	}
	if before, after := shrink["tail_before"].(float64), shrink["tail_after"].(float64); after >= before {
		t.Errorf("变短方向的字节数 = %v → %v, want 递减", before, after)
	}

	// 长度列缺失:不输出数字,避免界面把未知说成「改写」。
	missing := steps[4]
	if _, has := missing["tail_before"]; has {
		t.Errorf("长度缺失却输出了 tail_before: %+v", missing)
	}
	if _, has := missing["tail_after"]; has {
		t.Errorf("长度缺失却输出了 tail_after: %+v", missing)
	}
}

// TestAdmin_TraceStepPureAppendHasNoDiff 覆盖纯追加不产生差异块:
// 追加的内容由「本轮新增消息」呈现;若也算作分歧,徽标会把「追加」误标为「分叉」。
func TestAdmin_TraceStepPureAppendHasNoDiff(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mk := func(texts ...string) []trace.MainlineMessage {
		out := make([]trace.MainlineMessage, 0, len(texts))
		for _, s := range texts {
			out = append(out, trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": s}}})
		}
		return out
	}
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 100, mk("a", "b"))
	writeStep(t, store, "2026-10-13T10:00:10Z", 200, 100, mk("a", "b", "c", "d"))

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2", "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)
	if _, has := body["diff"]; has {
		t.Errorf("纯追加不应有差异块: %v", body["diff"])
	}
	if _, has := body["diff_lines"]; has {
		t.Errorf("纯追加不应有 diff_lines: %v", body["diff_lines"])
	}
	// 新增内容照常由 messages 呈现。
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Errorf("新增消息数 = %d, want 2", len(msgs))
	}
}

// TestAdmin_TraceDiffContext 覆盖标准 unified diff 的上下文行:
// 块头范围含上下文,且给出变更前后的上下文行(各至多 3 行、不侵入相邻块)。
func TestAdmin_TraceDiffContext(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mk := func(texts ...string) []trace.MainlineMessage {
		out := make([]trace.MainlineMessage, 0, len(texts))
		for _, s := range texts {
			out = append(out, trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": s}}})
		}
		return out
	}
	// 中间一行被改动,前后各有 4 行上下文(期望取 3 行)。
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 100,
		mk("c1", "c2", "c3", "c4", "OLD", "d1", "d2", "d3", "d4"))
	writeStep(t, store, "2026-10-13T10:00:10Z", 200, 100,
		mk("c1", "c2", "c3", "c4", "NEW", "d1", "d2", "d3", "d4"))

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2", "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)
	hunks, _ := body["diff"].([]any)
	if len(hunks) != 1 {
		t.Fatalf("块数 = %d, want 1", len(hunks))
	}
	h, _ := hunks[0].(map[string]any)

	// 块头:变更在第 5 行(0 基 4),上下各 3 行 → 范围从第 2 行起共 7 行。
	// 标准格式 `@@ -2,7 +2,7 @@`。
	if got := h["header"]; got != "@@ -2,7 +2,7 @@" {
		t.Errorf("块头 = %v, want @@ -2,7 +2,7 @@", got)
	}
	// 变更本身的范围不含上下文。
	if h["prev_start"] != float64(4) || h["prev_count"] != float64(1) {
		t.Errorf("变更范围 = %v+%v, want 4+1", h["prev_start"], h["prev_count"])
	}

	beforeRows, _ := h["ctx_before"].([]any)
	if len(beforeRows) != 3 {
		t.Fatalf("前置上下文 = %d 行, want 3", len(beforeRows))
	}
	// 前置上下文按时间顺序:第 2、3、4 行(0 基 1、2、3)。
	for i, raw := range beforeRows {
		row, _ := raw.(map[string]any)
		if row["prev_line"] != float64(1+i) {
			t.Errorf("前置第 %d 行的旧侧行号 = %v, want %d", i, row["prev_line"], 1+i)
		}
		if row["message"] == nil {
			t.Errorf("前置第 %d 行缺内容", i)
		}
	}
	afterRows, _ := h["ctx_after"].([]any)
	if len(afterRows) != 3 {
		t.Fatalf("后置上下文 = %d 行, want 3", len(afterRows))
	}
	first, _ := afterRows[0].(map[string]any)
	if first["prev_line"] != float64(5) {
		t.Errorf("后置首行的旧侧行号 = %v, want 5", first["prev_line"])
	}
}

// TestAdmin_TraceDiffContextClampedAtEdges 覆盖序列两端:上下文不得越界。
func TestAdmin_TraceDiffContextClampedAtEdges(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mk := func(texts ...string) []trace.MainlineMessage {
		out := make([]trace.MainlineMessage, 0, len(texts))
		for _, s := range texts {
			out = append(out, trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": s}}})
		}
		return out
	}
	// 首行即变更:前置上下文应为 0 行,块头从第 1 行起。
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 100, mk("OLD", "b", "c"))
	writeStep(t, store, "2026-10-13T10:00:10Z", 200, 100, mk("NEW", "b", "c"))

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2", "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)
	hunks, _ := body["diff"].([]any)
	h, _ := hunks[0].(map[string]any)
	if _, has := h["ctx_before"]; has {
		t.Errorf("首行变更不应有前置上下文: %v", h["ctx_before"])
	}
	// 变更在第 1 行、后置 2 行 → 共 3 行,起点为 1。
	if got := h["header"]; got != "@@ -1,3 +1,3 @@" {
		t.Errorf("块头 = %v, want @@ -1,3 +1,3 @@", got)
	}
}

// TestParseCtxLines 覆盖上下文行数参数:默认 3、非法取默认、上限封顶。
func TestParseCtxLines(t *testing.T) {
	cases := map[string]int{
		"":     diffContextDefault,
		"abc":  diffContextDefault,
		"-5":   diffContextDefault,
		"0":    0,
		"10":   10,
		"9999": diffContextMax,
	}
	for raw, want := range cases {
		if got := parseCtxLines(raw); got != want {
			t.Errorf("parseCtxLines(%q) = %d, want %d", raw, got, want)
		}
	}
}

// TestAdmin_TraceDiffContextParam 覆盖按请求展开上下文:ctx 增大时返回更多上下文行。
func TestAdmin_TraceDiffContextParam(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mk := func(texts ...string) []trace.MainlineMessage {
		out := make([]trace.MainlineMessage, 0, len(texts))
		for _, s := range texts {
			out = append(out, trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": s}}})
		}
		return out
	}
	// 中间一行被改动,前后各 10 行上下文。
	prev := []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "OLD"}
	cur := append(append([]string{}, prev[:10]...), "NEW")
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 100, mk(prev...))
	writeStep(t, store, "2026-10-13T10:00:10Z", 200, 100, mk(cur...))

	// 默认 3 行。
	var body traceJSON
	decode(t, do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2", "", cookie, false), &body)
	hunks, _ := body["diff"].([]any)
	h, _ := hunks[0].(map[string]any)
	if rows, _ := h["ctx_before"].([]any); len(rows) != 3 {
		t.Errorf("默认前置上下文 = %d 行, want 3", len(rows))
	}
	if body["ctx_lines"] != float64(3) {
		t.Errorf("ctx_lines = %v, want 3", body["ctx_lines"])
	}

	// ctx=8:前置上下文应扩展到 8 行(受序列开头限制,共 10 行可用)。
	body = traceJSON{}
	decode(t, do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2?ctx=8", "", cookie, false), &body)
	hunks, _ = body["diff"].([]any)
	h, _ = hunks[0].(map[string]any)
	if rows, _ := h["ctx_before"].([]any); len(rows) != 8 {
		t.Errorf("ctx=8 时前置上下文 = %d 行, want 8", len(rows))
	}
	if body["ctx_lines"] != float64(8) {
		t.Errorf("ctx_lines = %v, want 8", body["ctx_lines"])
	}
	// 块头范围随上下文扩张:变更在第 11 行(0 基 10),前置 8 行 → 从第 3 行起共 9 行。
	if got := h["header"]; got != "@@ -3,9 +3,9 @@" {
		t.Errorf("块头 = %v, want @@ -3,9 +3,9 @@", got)
	}

	// ctx=0:不带上下文。
	body = traceJSON{}
	decode(t, do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2?ctx=0", "", cookie, false), &body)
	hunks, _ = body["diff"].([]any)
	h, _ = hunks[0].(map[string]any)
	if _, has := h["ctx_before"]; has {
		t.Errorf("ctx=0 不应有上下文: %v", h["ctx_before"])
	}
	if got := h["header"]; got != "@@ -11 +11 @@" {
		t.Errorf("ctx=0 块头 = %v, want @@ -11 +11 @@(单行省略计数)", got)
	}
}

// TestAdmin_TraceDiffUnchangedBefore 覆盖块间未变更行数:
// 界面据此显示「… 未变更 N 行 …」,首块不带该字段。
func TestAdmin_TraceDiffUnchangedBefore(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	mk := func(texts ...string) []trace.MainlineMessage {
		out := make([]trace.MainlineMessage, 0, len(texts))
		for _, s := range texts {
			out = append(out, trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": s}}})
		}
		return out
	}
	// 两处差异,中间隔 5 行未变更。
	base := []string{"a", "X1", "b", "c", "d", "e", "f", "X2", "g"}
	changed := []string{"a", "Y1", "b", "c", "d", "e", "f", "Y2", "g"}
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 100, mk(base...))
	writeStep(t, store, "2026-10-13T10:00:10Z", 200, 100, mk(changed...))

	var body traceJSON
	decode(t, do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID+"/2?ctx=0", "", cookie, false), &body)
	hunks, _ := body["diff"].([]any)
	if len(hunks) != 2 {
		t.Fatalf("块数 = %d, want 2", len(hunks))
	}
	first, _ := hunks[0].(map[string]any)
	if _, has := first["unchanged_before"]; has {
		t.Errorf("首块不应带不变更计数: %v", first["unchanged_before"])
	}
	second, _ := hunks[1].(map[string]any)
	// 首块变更在第 2 行(0 基 1),次块在第 8 行(0 基 7),中间隔 5 行。
	if second["unchanged_before"] != float64(5) {
		t.Errorf("块间未变更行数 = %v, want 5", second["unchanged_before"])
	}
}
