package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"awatchdog/pkg/config/db"
	"awatchdog/pkg/trace"
)

// toolCall 是一条配好对读数的工具调用。
type toolCall struct {
	ToolCallID   string `json:"tool_call_id"`
	ToolName     string `json:"tool_name"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at,omitempty"`
	DurationMS   *int64 `json:"duration_ms,omitempty"`
	Status       string `json:"status,omitempty"`
	Boundary     string `json:"boundary"`
	ApprovalWait bool   `json:"approval_wait"`
	Complete     bool   `json:"complete"`
	// Input 与 Result 优先取客户端上报值;没有时为空。
	Input           any  `json:"input,omitempty"`
	Result          any  `json:"result,omitempty"`
	InputTruncated  bool `json:"input_truncated,omitempty"`
	ResultTruncated bool `json:"result_truncated,omitempty"`
}

// handleListToolEvents 返回某会话的工具调用列表:按 tool_call_id 配对开始与结束。
// 只有开始事件的调用标记 complete=false,不给出时长。
func (a *Admin) handleListToolEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	rows, err := a.store.ListToolEvents(r.Context(), sessionID)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pairToolEvents(rows))
}

// pairToolEvents 把事件行按 tool_call_id 折成调用列表,顺序按开始时间。
func pairToolEvents(rows []db.ToolEvent) []toolCall {
	order := make([]string, 0, len(rows))
	index := make(map[string]*toolCall, len(rows))
	for _, row := range rows {
		call, ok := index[row.ToolCallID]
		if !ok {
			call = &toolCall{
				ToolCallID:   row.ToolCallID,
				ToolName:     row.ToolName,
				Boundary:     row.Boundary,
				ApprovalWait: row.ApprovalWait != 0,
			}
			index[row.ToolCallID] = call
			order = append(order, row.ToolCallID)
		}
		switch row.Phase {
		case "start":
			if call.StartedAt == "" {
				call.StartedAt = row.Ts
			}
			if len(row.InputGzip) > 0 {
				call.Input = decodePayload(row.InputGzip, row.InputTruncated != 0)
				call.InputTruncated = row.InputTruncated != 0
			}
		case "end":
			if len(row.ResultGzip) > 0 {
				call.Result = decodePayload(row.ResultGzip, row.ResultTruncated != 0)
				call.ResultTruncated = row.ResultTruncated != 0
			}
			if call.Input == nil && len(row.InputGzip) > 0 {
				call.Input = decodePayload(row.InputGzip, row.InputTruncated != 0)
				call.InputTruncated = row.InputTruncated != 0
			}
			if call.FinishedAt != "" {
				continue
			}
			call.FinishedAt = row.Ts
			call.Status = row.Status
			if start, err := time.Parse(time.RFC3339Nano, call.StartedAt); err == nil {
				if end, err := time.Parse(time.RFC3339Nano, row.Ts); err == nil && !end.Before(start) {
					ms := end.Sub(start).Milliseconds()
					call.DurationMS = &ms
					call.Complete = true
				}
			}
		}
	}
	out := make([]toolCall, 0, len(order))
	for _, id := range order {
		out = append(out, *index[id])
	}
	return out
}

// decodePayload 解压并解析上报载荷;截断的内容可能不是合法 JSON,退回字符串。
func decodePayload(compressed []byte, truncated bool) any {
	raw, err := trace.Decompress(compressed)
	if err != nil {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		text := string(raw)
		if truncated {
			text += "…"
		}
		return text
	}
	return value
}
