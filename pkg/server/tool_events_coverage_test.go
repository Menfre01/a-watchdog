package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
	"awatchdog/pkg/trace"
)

// newToolEventsUnderTest 返回独立的事件端点与真实 Store。
// 上游地址不可达:事件路径不访问上游。
func newToolEventsUnderTest(t *testing.T) (*config.Store, *toolEvents) {
	t.Helper()
	store, _ := newTestStack(t, "http://127.0.0.1:1")
	return store, &toolEvents{store: store, snapshot: store.Snapshot}
}

// serveTool 直接调用事件端点;ctx 用于制造入库阶段的取消。
func serveTool(t *testing.T, h http.Handler, rawURL, body string, headers map[string]string, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// toolAuthHeaders 返回带有效 Bearer 凭据的请求头。
func toolAuthHeaders() map[string]string {
	return map[string]string{"Authorization": "Bearer " + testClientKey}
}

// canceledContext 返回已取消的请求上下文。
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestToolEventsServeHTTP_ErrorPaths 覆盖上报端点的鉴权、体积、JSON 与入库错误路径。
func TestToolEventsServeHTTP_ErrorPaths(t *testing.T) {
	const validBody = `{"hook_event_name":"PreToolUse","session_id":"s1","tool_use_id":"t1","tool_name":"Bash"}`

	cases := []struct {
		name     string
		body     string
		headers  map[string]string
		ctx      context.Context
		wantCode int
		wantMsg  string
	}{
		{
			name: "缺少凭据", body: validBody, headers: nil, ctx: context.Background(),
			wantCode: http.StatusUnauthorized, wantMsg: "invalid or disabled key",
		},
		{
			name: "请求体超过上限", body: strings.Repeat("x", toolEventsMaxBody+1),
			headers: toolAuthHeaders(), ctx: context.Background(),
			wantCode: http.StatusBadRequest, wantMsg: "request body too large or unreadable",
		},
		{
			name: "请求体不是合法 JSON", body: "not-json",
			headers: toolAuthHeaders(), ctx: context.Background(),
			wantCode: http.StatusBadRequest, wantMsg: "invalid JSON body",
		},
		{
			name: "缺少会话标识", body: `{"hook_event_name":"PreToolUse","tool_use_id":"t1"}`,
			headers: toolAuthHeaders(), ctx: context.Background(),
			wantCode: http.StatusBadRequest, wantMsg: "session_id is required",
		},
		{
			name: "入库失败", body: validBody, headers: toolAuthHeaders(), ctx: canceledContext(),
			wantCode: http.StatusInternalServerError, wantMsg: "store tool event failed",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, tools := newToolEventsUnderTest(t)
			rec := serveTool(t, tools, "/v1/tool-events?client=claude-code", c.body, c.headers, c.ctx)
			if rec.Code != c.wantCode {
				t.Fatalf("状态 = %d, want %d, body = %s", rec.Code, c.wantCode, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.wantMsg) {
				t.Errorf("响应体 = %s, want 含 %q", rec.Body.String(), c.wantMsg)
			}
		})
	}
}

// TestToolEventsIngest_TruncatesLargePayload 覆盖大载荷入库:压缩前截断并置标记,
// 同时覆盖 approval_wait 到 0/1 的转换。
func TestToolEventsIngest_TruncatesLargePayload(t *testing.T) {
	store, tools := newToolEventsUnderTest(t)

	payload, err := json.Marshal(map[string]any{
		"session_id":    "big",
		"turn_id":       "r1",
		"tool_call_id":  "t1",
		"tool_name":     "Bash",
		"phase":         "start",
		"approval_wait": true,
		"input":         strings.Repeat("a", toolPayloadLimit+64),
	})
	if err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}

	rec := serveTool(t, tools, "/v1/tool-events?client=dsh", string(payload), toolAuthHeaders(), context.Background())
	if rec.Code != http.StatusNoContent {
		t.Fatalf("状态 = %d, want 204, body = %s", rec.Code, rec.Body.String())
	}

	rows, err := store.ListToolEvents(context.Background(), "1:big")
	if err != nil {
		t.Fatalf("ListToolEvents() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("事件数 = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.ApprovalWait != 1 {
		t.Errorf("approval_wait = %d, want 1", row.ApprovalWait)
	}
	if row.InputTruncated != 1 {
		t.Errorf("input_truncated = %d, want 1", row.InputTruncated)
	}
	raw, err := trace.Decompress(row.InputGzip)
	if err != nil {
		t.Fatalf("解压入参失败: %v", err)
	}
	if len(raw) != toolPayloadLimit {
		t.Errorf("截断后长度 = %d, want %d", len(raw), toolPayloadLimit)
	}
}

// TestAdaptToolEvent 表驱动覆盖三类客户端适配、字段优先级与校验错误。
func TestAdaptToolEvent(t *testing.T) {
	cases := []struct {
		name    string
		client  string
		payload map[string]any
		want    toolEvent
		wantErr string
	}{
		{
			name:   "claude-code 开始事件",
			client: "claude-code",
			payload: map[string]any{
				"session_id": "s1", "turn_id": "r1", "tool_use_id": "t1", "tool_name": "Bash",
				"hook_event_name": "PreToolUse", "approval_wait": true,
				"tool_input": map[string]any{"command": "ls"},
			},
			want: toolEvent{
				Session: "s1", Turn: "r1", ToolCallID: "t1", ToolName: "Bash",
				Phase: "start", ApprovalWait: true,
				Input: map[string]any{"command": "ls"},
			},
		},
		{
			name:   "claude-code 失败结束事件",
			client: "claude-code",
			payload: map[string]any{
				"session_id": "s1", "tool_use_id": "t1",
				"hook_event_name": "PostToolUseFailure",
				"tool_response":   map[string]any{"stderr": "boom"},
			},
			want: toolEvent{
				Session: "s1", ToolCallID: "t1", ToolName: "unknown",
				Phase: "end", Status: "error",
				Result: map[string]any{"stderr": "boom"},
			},
		},
		{
			name:   "codex 成功结束事件",
			client: "codex",
			payload: map[string]any{
				"session_id": "s2", "tool_use_id": "t2", "tool_name": "Read",
				"hook_event_name": "PostToolUse",
			},
			want: toolEvent{Session: "s2", ToolCallID: "t2", ToolName: "Read", Phase: "end", Status: "ok"},
		},
		{
			name:   "dsh 适配",
			client: "dsh",
			payload: map[string]any{
				"session_id": "s3", "turn_id": "r3", "tool_call_id": "t3", "tool_name": "Grep",
				"phase": "end", "status": "ok",
				"input":  map[string]any{"q": "x"},
				"result": map[string]any{"n": 1},
			},
			want: toolEvent{
				Session: "s3", Turn: "r3", ToolCallID: "t3", ToolName: "Grep",
				Phase: "end", Status: "ok",
				Input:  map[string]any{"q": "x"},
				Result: map[string]any{"n": 1},
			},
		},
		{
			name:   "未知客户端按规范字段解析",
			client: "mystery",
			payload: map[string]any{
				"session_id": "s4", "prompt_id": "p4", "tool_call_id": "t4", "tool_name": "Edit",
				"phase": "start", "input": map[string]any{"path": "a"},
			},
			want: toolEvent{
				Session: "s4", Turn: "p4", ToolCallID: "t4", ToolName: "Edit",
				Phase: "start", Input: map[string]any{"path": "a"},
			},
		},
		{
			name:   "入参与结果字段优先级",
			client: "canonical",
			payload: map[string]any{
				"session_id": "s5", "tool_call_id": "t5", "phase": "start",
				"tool_input":    map[string]any{"first": true},
				"input":         map[string]any{"second": true},
				"tool_response": map[string]any{"primary": true},
				"result":        map[string]any{"fallback": true},
				"error":         map[string]any{"last": true},
			},
			want: toolEvent{
				Session: "s5", ToolCallID: "t5", ToolName: "unknown", Phase: "start",
				Input:  map[string]any{"first": true},
				Result: map[string]any{"primary": true},
			},
		},
		{
			name:    "缺少会话标识",
			client:  "canonical",
			payload: map[string]any{"tool_call_id": "t", "phase": "start"},
			wantErr: "session_id is required",
		},
		{
			name:    "缺少调用标识",
			client:  "canonical",
			payload: map[string]any{"session_id": "s", "phase": "start"},
			wantErr: "tool_use_id is required",
		},
		{
			name:    "阶段非法",
			client:  "canonical",
			payload: map[string]any{"session_id": "s", "tool_call_id": "t", "phase": "sideways"},
			wantErr: "phase must be start or end",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := adaptToolEvent(c.client, c.payload)
			if c.wantErr != "" {
				if err == nil || err.Error() != c.wantErr {
					t.Fatalf("error = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("adaptToolEvent() error = %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("事件 = %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestPackPayload 表驱动覆盖普通压缩、超限截断与不可序列化值。
func TestPackPayload(t *testing.T) {
	large := strings.Repeat("a", toolPayloadLimit+10)

	cases := []struct {
		name          string
		value         any
		wantNil       bool
		wantTruncated int64
		wantRaw       string
		wantRawLen    int
	}{
		{
			name: "普通载荷", value: map[string]any{"cmd": "ls"},
			wantTruncated: 0, wantRaw: `{"cmd":"ls"}`,
		},
		{
			name: "超限载荷截断", value: large,
			wantTruncated: 1, wantRawLen: toolPayloadLimit,
		},
		{
			name: "不可序列化的值", value: make(chan int),
			wantNil: true, wantTruncated: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, truncated := packPayload(c.value)
			if (got == nil) != c.wantNil {
				t.Fatalf("nil = %v, want %v", got == nil, c.wantNil)
			}
			if truncated != c.wantTruncated {
				t.Fatalf("truncated = %d, want %d", truncated, c.wantTruncated)
			}
			if c.wantNil {
				return
			}
			if len(got) == 0 {
				t.Fatal("压缩结果为空")
			}
			raw, err := trace.Decompress(got)
			if err != nil {
				t.Fatalf("解压失败: %v", err)
			}
			if c.wantRawLen > 0 && len(raw) != c.wantRawLen {
				t.Errorf("解压长度 = %d, want %d", len(raw), c.wantRawLen)
			}
			if c.wantRaw != "" && string(raw) != c.wantRaw {
				t.Errorf("解压内容 = %s, want %s", raw, c.wantRaw)
			}
		})
	}
}

// TestToolEventsAuthVariants 覆盖两种凭据头、大小写不敏感与停用密钥。
func TestToolEventsAuthVariants(t *testing.T) {
	const disabledKey = "awd_disabled_test_key"

	store, tools := newToolEventsUnderTest(t)
	if _, err := store.CreateClientKey(context.Background(), db.CreateClientKeyParams{
		Name: "disabled", KeyHash: config.HashClientKey(disabledKey), KeyPrefix: "awd_", Enabled: 0,
	}); err != nil {
		t.Fatalf("CreateClientKey() error = %v", err)
	}

	const body = `{"hook_event_name":"PreToolUse","session_id":"auth","tool_use_id":"t1","tool_name":"Bash"}`
	cases := []struct {
		name     string
		headers  map[string]string
		wantCode int
	}{
		{
			name:     "Bearer 有效",
			headers:  map[string]string{"Authorization": "Bearer " + testClientKey},
			wantCode: http.StatusNoContent,
		},
		{
			name:     "Bearer 前缀大小写不敏感",
			headers:  map[string]string{"Authorization": "bearer " + testClientKey},
			wantCode: http.StatusNoContent,
		},
		{
			name:     "x-api-key 有效",
			headers:  map[string]string{"x-api-key": testClientKey},
			wantCode: http.StatusNoContent,
		},
		{
			name:     "密钥错误",
			headers:  map[string]string{"Authorization": "Bearer wrong"},
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "凭据前缀非 Bearer",
			headers:  map[string]string{"Authorization": "Basic " + testClientKey},
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "密钥已停用",
			headers:  map[string]string{"Authorization": "Bearer " + disabledKey},
			wantCode: http.StatusUnauthorized,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := serveTool(t, tools, "/v1/tool-events?client=claude-code", body, c.headers, context.Background())
			if rec.Code != c.wantCode {
				t.Fatalf("状态 = %d, want %d, body = %s", rec.Code, c.wantCode, rec.Body.String())
			}
		})
	}
}

// TestPromptEvents_ErrorPaths 覆盖提示上报的鉴权、体积、JSON、必填字段与入库错误。
func TestPromptEvents_ErrorPaths(t *testing.T) {
	const validBody = `{"session_id":"p1","prompt_id":"pr1","prompt":"hi"}`

	cases := []struct {
		name     string
		body     string
		headers  map[string]string
		ctx      context.Context
		wantCode int
		wantMsg  string
	}{
		{
			name: "缺少凭据", body: validBody, headers: nil, ctx: context.Background(),
			wantCode: http.StatusUnauthorized, wantMsg: "invalid or disabled key",
		},
		{
			name: "请求体超过上限", body: strings.Repeat("x", toolEventsMaxBody+1),
			headers: toolAuthHeaders(), ctx: context.Background(),
			wantCode: http.StatusBadRequest, wantMsg: "request body too large or unreadable",
		},
		{
			name: "请求体不是合法 JSON", body: "not-json",
			headers: toolAuthHeaders(), ctx: context.Background(),
			wantCode: http.StatusBadRequest, wantMsg: "invalid JSON body",
		},
		{
			name: "缺少会话标识", body: `{"prompt_id":"pr1"}`,
			headers: toolAuthHeaders(), ctx: context.Background(),
			wantCode: http.StatusBadRequest, wantMsg: "session_id is required",
		},
		{
			name: "入库失败", body: validBody, headers: toolAuthHeaders(), ctx: canceledContext(),
			wantCode: http.StatusInternalServerError, wantMsg: "store prompt event failed",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, tools := newToolEventsUnderTest(t)
			rec := serveTool(t, http.HandlerFunc(tools.handlePrompt),
				"/v1/prompt-events?client=claude-code", c.body, c.headers, c.ctx)
			if rec.Code != c.wantCode {
				t.Fatalf("状态 = %d, want %d, body = %s", rec.Code, c.wantCode, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.wantMsg) {
				t.Errorf("响应体 = %s, want 含 %q", rec.Body.String(), c.wantMsg)
			}
		})
	}
}
