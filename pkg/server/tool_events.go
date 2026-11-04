package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
	"awatchdog/pkg/trace"
)

// toolEventsMaxBody 是上报体的上限:事件只含标识与少量字段。
const toolEventsMaxBody = 1 << 20

// toolEvents 接收客户端工具事件上报。三类客户端共用同一端点,
// 由 client 查询参数选择字段适配;口径与幂等规则见
// openspec/changes/tool-events-ingest 的规格。
type toolEvents struct {
	store    *config.Store
	snapshot func() *config.Snapshot
}

// toolEvent 是适配后的内部事件。
type toolEvent struct {
	Session      string
	Turn         string
	ToolCallID   string
	ToolName     string
	Phase        string
	Status       string
	ApprovalWait bool
	// Input 与 Result 是客户端上报的原始 JSON;没有时为空。
	Input  any
	Result any
}

// toolPayloadLimit 是入参或结果的单字段上限(压缩前):256 KB。
const toolPayloadLimit = 256 << 10

// ServeHTTP 校验客户端密钥、适配载荷并幂等入库。
func (t *toolEvents) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	info, ok := t.auth(r)
	if !ok {
		writeToolError(w, http.StatusUnauthorized, "invalid or disabled key")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, toolEventsMaxBody))
	if err != nil {
		writeToolError(w, http.StatusBadRequest, "request body too large or unreadable")
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeToolError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	event, err := adaptToolEvent(r.URL.Query().Get("client"), payload)
	if err != nil {
		writeToolError(w, http.StatusBadRequest, err.Error())
		return
	}

	params := db.InsertToolEventParams{
		Client:       strings.TrimSpace(r.URL.Query().Get("client")),
		SessionID:    strconv.FormatInt(info.ID, 10) + ":" + event.Session,
		TurnID:       event.Turn,
		ToolCallID:   event.ToolCallID,
		ToolName:     event.ToolName,
		Phase:        event.Phase,
		Status:       event.Status,
		Boundary:     "hook",
		ApprovalWait: boolToInt(event.ApprovalWait),
		Ts:           time.Now().UTC().Format(time.RFC3339Nano),
	}
	if event.Input != nil {
		params.InputGzip, params.InputTruncated = packPayload(event.Input)
	}
	if event.Result != nil {
		params.ResultGzip, params.ResultTruncated = packPayload(event.Result)
	}
	if err := t.store.InsertToolEvent(r.Context(), params); err != nil {
		writeToolError(w, http.StatusInternalServerError, "store tool event failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adaptToolEvent 把各客户端载荷折成内部事件。
// 未识别的客户端按规范字段解析,便于新客户端先接入后补适配。
func adaptToolEvent(client string, payload map[string]any) (toolEvent, error) {
	event := toolEvent{
		Session:    str(payload["session_id"]),
		Turn:       str(payload["turn_id"]),
		ToolCallID: str(payload["tool_use_id"]),
		ToolName:   str(payload["tool_name"]),
	}
	if event.Turn == "" {
		event.Turn = str(payload["prompt_id"])
	}
	switch client {
	case "claude-code", "codex":
		switch str(payload["hook_event_name"]) {
		case "PreToolUse":
			event.Phase = "start"
		case "PostToolUse":
			event.Phase = "end"
			event.Status = "ok"
		case "PostToolUseFailure":
			event.Phase = "end"
			event.Status = "error"
		}
	case "dsh":
		event.ToolCallID = str(payload["tool_call_id"])
		event.Phase = str(payload["phase"])
		event.Status = str(payload["status"])
	default:
		event.ToolCallID = str(payload["tool_call_id"])
		event.Phase = str(payload["phase"])
		event.Status = str(payload["status"])
	}
	event.ApprovalWait = payload["approval_wait"] == true
	event.Input = firstOf(payload, "tool_input", "input")
	event.Result = firstOf(payload, "tool_response", "result", "error")

	if event.Session == "" {
		return toolEvent{}, fmt.Errorf("session_id is required")
	}
	if event.ToolCallID == "" {
		return toolEvent{}, fmt.Errorf("tool_use_id is required")
	}
	if event.Phase != "start" && event.Phase != "end" {
		return toolEvent{}, fmt.Errorf("phase must be start or end")
	}
	if event.ToolName == "" {
		event.ToolName = "unknown"
	}
	return event, nil
}

// toolBearer 从 Authorization 头提取 Bearer 凭据(大小写不敏感)。
func toolBearer(h string) string {
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}

// str 读取字符串字段;缺失或类型不符时返回空串。
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// boolToInt 把布尔转成 SQLite 的 0/1。
func boolToInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// writeToolError 写出最简 JSON 错误。
func writeToolError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":%q}`, message)
}

// auth 校验客户端密钥,返回密钥信息。
func (t *toolEvents) auth(r *http.Request) (config.KeyInfo, bool) {
	raw := toolBearer(r.Header.Get("Authorization"))
	if raw == "" {
		raw = r.Header.Get("x-api-key")
	}
	if raw == "" {
		return config.KeyInfo{}, false
	}
	info, ok := t.snapshot().LookupKey(config.HashClientKey(raw))
	if !ok || !info.Enabled {
		return config.KeyInfo{}, false
	}
	return info, true
}

// handlePrompt 接收提示提交事件:只存时刻与长度,不存正文。
func (t *toolEvents) handlePrompt(w http.ResponseWriter, r *http.Request) {
	info, ok := t.auth(r)
	if !ok {
		writeToolError(w, http.StatusUnauthorized, "invalid or disabled key")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, toolEventsMaxBody))
	if err != nil {
		writeToolError(w, http.StatusBadRequest, "request body too large or unreadable")
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeToolError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	session := str(payload["session_id"])
	promptID := str(payload["prompt_id"])
	if session == "" {
		writeToolError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	promptLen := int64(0)
	if text, ok := payload["prompt"].(string); ok {
		promptLen = int64(len(text))
	}
	params := db.InsertPromptEventParams{
		Client:    strings.TrimSpace(r.URL.Query().Get("client")),
		SessionID: strconv.FormatInt(info.ID, 10) + ":" + session,
		TurnID:    str(payload["turn_id"]),
		PromptID:  promptID,
		PromptLen: promptLen,
		Ts:        time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := t.store.InsertPromptEvent(r.Context(), params); err != nil {
		writeToolError(w, http.StatusInternalServerError, "store prompt event failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// firstOf 返回第一个非空字段:不同客户端对入参与结果的命名不同。
func firstOf(payload map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := payload[key]; ok && v != nil {
			return v
		}
	}
	return nil
}

// packPayload 把载荷转成 gzip;超过上限时只存前缀并标记截断。
func packPayload(value any) ([]byte, int64) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, 0
	}
	truncated := int64(0)
	if len(raw) > toolPayloadLimit {
		raw = raw[:toolPayloadLimit]
		truncated = 1
	}
	compressed, err := trace.Compress(raw)
	if err != nil {
		return nil, truncated
	}
	return compressed, truncated
}
