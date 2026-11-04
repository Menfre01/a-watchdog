package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
	"awatchdog/pkg/trace"
)

// TestParseResponse 覆盖响应体的三种形态:Anthropic JSON、OpenAI JSON 与 SSE。
func TestParseResponse(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want map[string]any
	}{
		{
			name: "Anthropic 非流式",
			raw: `{"content":[
				{"type":"text","text":"回答"},
				{"type":"thinking","thinking":"思考"},
				{"type":"tool_use","name":"Read","input":{"path":"a.go"}},
				{"type":"unknown"}
			],"usage":{"input_tokens":11,"output_tokens":22,"cache_read_input_tokens":33},"stop_reason":"end_turn"}`,
			want: map[string]any{
				"text": "回答", "thinking": "思考", "stop_reason": "end_turn",
			},
		},
		{
			name: "OpenAI 非流式",
			raw: `{"choices":[{"message":{"content":"hi","tool_calls":[
				{"function":{"name":"Bash"}},
				{"function":{}},
				"junk"
			]},"finish_reason":"tool_calls"}]}`,
			want: map[string]any{"text": "hi", "stop_reason": "tool_calls"},
		},
		{
			name: "SSE 前缀 data:",
			raw:  "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"流\"}}\n",
			want: map[string]any{"text": "流"},
		},
		{
			name: "SSE 前缀 event:",
			raw:  "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n",
			want: map[string]any{"usage_in": int64(7)},
		},
		{
			name: "非法 JSON 原样返回",
			raw:  "not-json",
			want: map[string]any{"text": "not-json"},
		},
		{
			name: "合法 JSON 但无 content/choices",
			raw:  `{"id":"x"}`,
			want: map[string]any{"text": `{"id":"x"}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseResponse([]byte(tc.raw))
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s = %v(%T), want %v(%T)", key, got[key], got[key], want, want)
				}
			}
		})
	}
}

// TestParseResponseAnthropicDetail 覆盖 Anthropic 分支的拼接、工具名与用量。
func TestParseResponseAnthropicDetail(t *testing.T) {
	raw := `{"content":[
		"junk",
		{"type":"text","text":"A"},
		{"type":"text","text":"B"},
		{"type":"thinking","thinking":"想一想"},
		{"type":"tool_use","name":"Read","input":{"path":"a.go"}}
	],"usage":{"input_tokens":11,"output_tokens":22,"cache_read_input_tokens":33},"stop_reason":"end_turn"}`

	got := parseResponse([]byte(raw))
	if got["text"] != "AB" {
		t.Errorf("text = %v, want AB", got["text"])
	}
	if got["thinking"] != "想一想" {
		t.Errorf("thinking = %v", got["thinking"])
	}
	tools, _ := got["tool_uses"].([]string)
	if len(tools) != 1 || tools[0] != "Read" {
		t.Errorf("tool_uses = %v", got["tool_uses"])
	}
	blocks, _ := got["blocks"].([]map[string]any)
	// 空白文本与空白思考被丢弃;非 map 元素被跳过。
	if len(blocks) != 4 {
		t.Errorf("blocks = %v, want 4(文本 2 + 思考 1 + 工具 1,空白被丢弃)", blocks)
	}
	if got["usage_in"] != int64(11) || got["usage_out"] != int64(22) || got["usage_cache"] != int64(33) {
		t.Errorf("usage = %v/%v/%v", got["usage_in"], got["usage_out"], got["usage_cache"])
	}
}

// TestParseSSE 覆盖流式事件累积:文本、思考、工具入参、用量与 OpenAI 兼容分片。
func TestParseSSE(t *testing.T) {
	raw := strings.Join([]string{
		"event: ping",
		"data: ",
		"data: [DONE]",
		"data: not-json",
		`data: {"type":"content_block_start","content_block":{"type":"text"}}`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"你"}}`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"好"}}`,
		`data: {"type":"content_block_start","content_block":{"type":"thinking"}}`,
		`data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"想"}}`,
		`data: {"type":"content_block_start","content_block":{"type":"tool_use","name":"Read","input":{"path":"a.go"}}}`,
		`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"x\":"}}`,
		`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"1}"}}`,
		`data: {"type":"content_block_start","content_block":{"type":"unknown"}}`,
		`data: {"type":"content_block_delta","delta":{"type":"unknown"}}`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":3}}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}`,
		`data: {"choices":[{"delta":{"content":"补充","tool_calls":[{"function":{"name":"Bash"}}]},"finish_reason":"stop"}]}`,
		`data: {"choices":[]}`,
	}, "\n")

	got := parseSSE(raw)
	if got["text"] != "你好补充" {
		t.Errorf("text = %v, want 你好补充", got["text"])
	}
	if got["thinking"] != "想" {
		t.Errorf("thinking = %v", got["thinking"])
	}
	tools, _ := got["tool_uses"].([]string)
	if len(tools) != 2 || tools[0] != "Read" || tools[1] != "Bash" {
		t.Errorf("tool_uses = %v", got["tool_uses"])
	}
	blocks, _ := got["blocks"].([]map[string]any)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %v, want 3", blocks)
	}
	tool := blocks[2]
	input, _ := tool["input"].(map[string]any)
	if input["x"] != float64(1) {
		t.Errorf("工具入参 = %#v, want 解析后的对象", tool["input"])
	}
	if got["stop_reason"] != "stop" {
		t.Errorf("stop_reason = %v, want stop", got["stop_reason"])
	}
	if got["usage_in"] != int64(10) || got["usage_out"] != int64(9) || got["usage_cache"] != int64(3) {
		t.Errorf("usage = %v/%v/%v", got["usage_in"], got["usage_out"], got["usage_cache"])
	}
}

// TestParseSSEInvalidToolInput 覆盖入参片段不是合法 JSON:保留原文。
func TestParseSSEInvalidToolInput(t *testing.T) {
	raw := `data: {"type":"content_block_start","content_block":{"type":"tool_use","name":"Read"}}
data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{bad"}}`
	got := parseSSE(raw)
	blocks, _ := got["blocks"].([]map[string]any)
	if len(blocks) != 1 || blocks[0]["input"] != "{bad" {
		t.Fatalf("blocks = %#v, want 原文保留", blocks)
	}
}

// TestStringFieldValueAndNumberField 覆盖字段读取的缺失与类型不符。
func TestStringFieldValueAndNumberField(t *testing.T) {
	if got := stringFieldValue("x"); got != "x" {
		t.Errorf("stringFieldValue(string) = %q", got)
	}
	if got := stringFieldValue(42); got != "" {
		t.Errorf("stringFieldValue(int) = %q, want 空串", got)
	}
	if got := numberField(nil, "x"); got != 0 {
		t.Errorf("numberField(nil) = %d", got)
	}
	if got := numberField(map[string]any{"x": 3}, "x"); got != 0 {
		t.Errorf("numberField(int) = %d, want 0", got)
	}
	if got := numberField(map[string]any{"x": float64(3)}, "x"); got != 3 {
		t.Errorf("numberField(float64) = %d, want 3", got)
	}
	if got := numberField(map[string]any{}, "x"); got != 0 {
		t.Errorf("numberField(missing) = %d, want 0", got)
	}
}

// TestBlockHelpers 覆盖流式块拼接、工具入参收尾与空块丢弃。
func TestBlockHelpers(t *testing.T) {
	blocks := []map[string]any{}
	appendBlockText(&blocks, "text", "text", "a")
	appendBlockText(&blocks, "text", "text", "b")
	appendBlockText(&blocks, "thinking", "thinking", "c")
	blocks = append(blocks, map[string]any{"type": "tool_use", "name": "Read"})
	if len(blocks) != 3 || blocks[0]["text"] != "ab" || blocks[1]["thinking"] != "c" {
		t.Fatalf("appendBlockText = %#v", blocks)
	}
	appendToolInputJSON(&blocks, "{\"x\":")
	appendToolInputJSON(&blocks, "1}")
	if blocks[2]["input_json"] != `{"x":1}` {
		t.Errorf("appendToolInputJSON 未累积: %#v", blocks[2])
	}
	// 没有 tool_use 块时是空操作。
	noTool := []map[string]any{{"type": "text", "text": "x"}}
	appendToolInputJSON(&noTool, "{}")
	if _, has := noTool[0]["input_json"]; has {
		t.Errorf("文本块不应获得 input_json: %#v", noTool)
	}

	finalizeToolInputs(blocks)
	if len(blocks) != 3 || blocks[2]["input"] == nil {
		t.Fatalf("finalizeToolInputs 未解析: %#v", blocks)
	}
	if _, has := blocks[2]["input_json"]; has {
		t.Errorf("input_json 未被删除: %#v", blocks[2])
	}
	invalid := []map[string]any{{"type": "tool_use", "input_json": "{bad"}}
	finalizeToolInputs(invalid)
	if invalid[0]["input"] != "{bad" {
		t.Errorf("非法入参未保留原文: %#v", invalid)
	}
	empty := []map[string]any{{"type": "tool_use", "input_json": ""}}
	finalizeToolInputs(empty)
	if _, has := empty[0]["input_json"]; has {
		t.Errorf("空 input_json 未删除: %#v", empty)
	}

	dropped := dropEmptyBlocks([]map[string]any{
		{"type": "thinking", "thinking": "  "},
		{"type": "text", "text": "  "},
		{"type": "thinking", "thinking": "有内容"},
		{"type": "text", "text": "有内容"},
		{"type": "tool_use", "name": "Read"},
	})
	if len(dropped) != 3 {
		t.Fatalf("dropEmptyBlocks = %#v, want 3", dropped)
	}
}

// writeStepWithRequestID 写入一条带快照的请求并返回请求 id(测试夹具)。
func writeStepWithRequestID(t *testing.T, store *config.Store, ts string, status, latency int64,
	msgs []trace.MainlineMessage,
) (requestID, seq int64) {
	t.Helper()
	ctx := context.Background()
	usage := int64(100)
	ids, err := store.InsertRequests(ctx, []db.InsertRequestParams{{
		Ts: ts, Protocol: "anthropic-messages", RequestedModel: "claude-sonnet-4-5",
		StatusCode: status, LatencyMs: latency, Stream: 1, UsageIn: &usage,
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
	seq, err = store.NextSnapshotSeq(ctx, testSessionID)
	if err != nil {
		t.Fatalf("NextSnapshotSeq() error = %v", err)
	}
	err = store.SaveTrace(ctx, db.InsertRequestBodyParams{
		RequestID: ids[0], Method: "POST", Path: "/v1/messages",
		BodyGzip: compressed, Headers: `{"x-claude-code-session-id":["sess-a"]}`,
	}, &config.TraceSnapshot{
		SessionID: testSessionID, SessionSource: "explicit",
		SessionNote: "explicit: x-claude-code-session-id",
		Seq:         seq, Ts: ts, NdjsonGzip: compressed,
		LineHashes: serialized.LineHashes, LineSizes: serialized.LineSizes,
	})
	if err != nil {
		t.Fatalf("SaveTrace() error = %v", err)
	}
	return ids[0], seq
}

// TestAdmin_TraceStepResponse 覆盖响应原文回显:正常、SSE、损坏与缺失。
func TestAdmin_TraceStepResponse(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	ctx := context.Background()
	msgs := []trace.MainlineMessage{{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": "hi"}}}}

	// 1) Anthropic JSON:truncated/complete 原样透出。
	req1, seq1 := writeStepWithRequestID(t, store, "2026-10-13T10:00:00Z", 200, 100, msgs)
	raw1 := []byte(`{"content":[{"type":"text","text":"回答"}],"usage":{"input_tokens":1}}`)
	gz1, err := trace.Compress(raw1)
	if err != nil {
		t.Fatalf("Compress() error = %v", err)
	}
	if err := store.InsertResponseBody(ctx, db.InsertResponseBodyParams{
		RequestID: req1, BodyGzip: gz1, Truncated: 1, Complete: 0,
	}); err != nil {
		t.Fatalf("InsertResponseBody() error = %v", err)
	}

	// 2) SSE 响应。
	req2, seq2 := writeStepWithRequestID(t, store, "2026-10-13T10:00:10Z", 200, 100, msgs)
	gz2, err := trace.Compress([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"流\"}}\n"))
	if err != nil {
		t.Fatalf("Compress() error = %v", err)
	}
	if err := store.InsertResponseBody(ctx, db.InsertResponseBodyParams{RequestID: req2, BodyGzip: gz2, Complete: 1}); err != nil {
		t.Fatalf("InsertResponseBody() error = %v", err)
	}

	// 3) 损坏的 gzip。
	req3, seq3 := writeStepWithRequestID(t, store, "2026-10-13T10:00:20Z", 200, 100, msgs)
	if err := store.InsertResponseBody(ctx, db.InsertResponseBodyParams{RequestID: req3, BodyGzip: []byte("bad")}); err != nil {
		t.Fatalf("InsertResponseBody() error = %v", err)
	}

	// 4) 没有响应记录。
	_, seq4 := writeStepWithRequestID(t, store, "2026-10-13T10:00:30Z", 200, 100, msgs)

	get := func(seq int64) map[string]any {
		t.Helper()
		resp := do(t, srv.Client(), http.MethodGet,
			srv.URL+"/console/api/traces/"+testSessionID+"/"+itoa(seq), "", cookie, false)
		var body map[string]any
		decode(t, resp, &body)
		out, _ := body["response"].(map[string]any)
		return out
	}

	got1 := get(seq1)
	if got1["recorded"] != true || got1["text"] != "回答" || got1["truncated"] != true || got1["complete"] != false {
		t.Errorf("JSON 响应 = %#v", got1)
	}
	got2 := get(seq2)
	if got2["recorded"] != true || got2["text"] != "流" {
		t.Errorf("SSE 响应 = %#v", got2)
	}
	got3 := get(seq3)
	if got3["recorded"] != false {
		t.Errorf("损坏 gzip 应标未记录: %#v", got3)
	}
	got4 := get(seq4)
	if got4["recorded"] != false {
		t.Errorf("缺失响应应标未记录: %#v", got4)
	}
}
