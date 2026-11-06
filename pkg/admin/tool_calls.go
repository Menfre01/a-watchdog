package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"awatchdog/pkg/trace"
)

// toolCallSummary 是调用明细列表的一行。完整入参与结果只在详情接口返回。
type toolCallSummary struct {
	ToolCallID   string `json:"tool_call_id"`
	ToolName     string `json:"tool_name"`
	IssuedSeq    int64  `json:"issued_seq"`
	ResultSeq    int64  `json:"result_seq"`
	Source       string `json:"source"`
	Status       string `json:"status"`
	StartedAt    string `json:"started_at"`
	DurationMS   *int64 `json:"duration_ms,omitempty"`
	ApprovalWait bool   `json:"approval_wait"`
	Complete     bool   `json:"complete"`
	InputPreview string `json:"input_preview"`
}

// toolCallDetail 是单次调用的完整载荷。
type toolCallDetail struct {
	toolCallSummary
	FinishedAt     string `json:"finished_at,omitempty"`
	Boundary       string `json:"boundary,omitempty"`
	Input          any    `json:"input,omitempty"`
	Result         any    `json:"result,omitempty"`
	InputTruncated bool   `json:"input_truncated,omitempty"`
	ResultTrunc    bool   `json:"result_truncated,omitempty"`
}

func (a *Admin) handleListToolCalls(w http.ResponseWriter, r *http.Request) {
	analysis, err := a.analyzeSession(r.Context(), r.PathValue("session_id"))
	if err != nil {
		a.traceErr(w, err)
		return
	}
	events, err := a.listToolCallEvents(r)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mergeToolCalls(analysis.tools, events, stepTimes(analysis)))
}

func (a *Admin) handleToolCallDetail(w http.ResponseWriter, r *http.Request) {
	analysis, err := a.analyzeSession(r.Context(), r.PathValue("session_id"))
	if err != nil {
		a.traceErr(w, err)
		return
	}
	events, err := a.listToolCallEvents(r)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	detail, ok := mergeToolCallDetail(analysis.tools, events, stepTimes(analysis), r.PathValue("tool_call_id"))
	if !ok {
		writeJSONError(w, http.StatusNotFound, "tool call not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *Admin) listToolCallEvents(r *http.Request) ([]toolCall, error) {
	rows, err := a.store.ListToolEvents(r.Context(), r.PathValue("session_id"))
	if err != nil {
		return nil, err
	}
	return pairToolEvents(rows), nil
}

func stepTimes(analysis *sessionAnalysis) map[int64]string {
	out := make(map[int64]string, len(analysis.steps))
	for _, step := range analysis.steps {
		out[step.Seq] = step.Ts
	}
	return out
}

func mergeToolCalls(calls []trace.ToolCall, events []toolCall, times map[int64]string) []toolCallSummary {
	byID := make(map[string]toolCall, len(events))
	for _, event := range events {
		byID[event.ToolCallID] = event
	}
	seen := make(map[string]bool, len(calls))
	out := make([]toolCallSummary, 0, len(calls)+len(events))
	for _, call := range calls {
		event, hasEvent := byID[call.ID]
		if hasEvent {
			seen[call.ID] = true
		}
		out = append(out, summaryOf(call, event, hasEvent, times))
	}
	for _, event := range events {
		if seen[event.ToolCallID] {
			continue
		}
		out = append(out, summaryOf(trace.ToolCall{}, event, true, times))
	}
	return out
}

func summaryOf(call trace.ToolCall, event toolCall, hasEvent bool, times map[int64]string) toolCallSummary {
	name := call.Name
	if name == "" {
		name = event.ToolName
	}
	status := string(call.Status)
	if status == "" && hasEvent {
		status = eventStatus(event)
	}
	started := event.StartedAt
	if started == "" {
		started = times[call.IssuedSeq]
	}
	var duration *int64
	if hasEvent {
		duration = event.DurationMS
	}
	input := any(nil)
	if hasEvent {
		input = event.Input
	}
	if input == nil {
		input = call.Input
	}
	id := call.ID
	if id == "" && hasEvent {
		id = event.ToolCallID
	}
	return toolCallSummary{
		ToolCallID:   id,
		ToolName:     name,
		IssuedSeq:    call.IssuedSeq,
		ResultSeq:    call.ResultSeq,
		Source:       toolCallSource(name),
		Status:       status,
		StartedAt:    started,
		DurationMS:   duration,
		ApprovalWait: hasEvent && event.ApprovalWait,
		Complete:     hasEvent && event.Complete,
		InputPreview: inputPreview(input),
	}
}

func mergeToolCallDetail(calls []trace.ToolCall, events []toolCall, times map[int64]string, id string) (toolCallDetail, bool) {
	var traceCall *trace.ToolCall
	for i := range calls {
		if calls[i].ID == id {
			traceCall = &calls[i]
			break
		}
	}
	var event *toolCall
	for i := range events {
		if events[i].ToolCallID == id {
			event = &events[i]
			break
		}
	}
	if traceCall == nil && event == nil {
		return toolCallDetail{}, false
	}
	var base trace.ToolCall
	if traceCall != nil {
		base = *traceCall
	}
	if event != nil {
		summary := summaryOf(base, *event, true, times)
		return toolCallDetail{
			toolCallSummary: summary,
			FinishedAt:      event.FinishedAt,
			Boundary:        event.Boundary,
			Input:           firstPayload(event.Input, base.Input),
			Result:          firstPayload(event.Result, base.Result),
			InputTruncated:  event.InputTruncated,
			ResultTrunc:     event.ResultTruncated,
		}, true
	}
	summary := summaryOf(base, toolCall{}, false, times)
	return toolCallDetail{
		toolCallSummary: summary,
		Input:           base.Input,
		Result:          base.Result,
	}, true
}

func firstPayload(primary, fallback any) any {
	if primary != nil {
		return primary
	}
	return fallback
}

func toolCallSource(name string) string {
	if strings.HasPrefix(name, "mcp__") {
		return "mcp"
	}
	return "local"
}

func eventStatus(event toolCall) string {
	if !event.Complete {
		return string(trace.ToolUnseen)
	}
	if event.Status == "" {
		return string(trace.ToolOK)
	}
	return event.Status
}

func inputPreview(input any) string {
	if input == nil {
		return "—"
	}
	switch v := input.(type) {
	case string:
		return oneLine(v, 72)
	case map[string]any:
		for _, key := range []string{"command", "file_path", "path", "pattern", "query", "url", "description", "prompt"} {
			if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
				return oneLine(s, 72)
			}
		}
		for key, value := range v {
			if s, ok := value.(string); ok {
				return oneLine(key+": "+s, 72)
			}
			return oneLine(key+": "+jsonText(value), 72)
		}
	}
	return oneLine(jsonText(input), 72)
}

func jsonText(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

func oneLine(text string, limit int) string {
	flat := strings.Join(strings.Fields(text), " ")
	return truncateRunes(flat, limit)
}
