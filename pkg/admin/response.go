package admin

import (
	"context"
	"encoding/json"
	"strings"

	"awatchdog/pkg/trace"
)

// responseOf 读取某请求的上游响应原文,并折成可展示的结构。
// 没有记录时返回 recorded=false,不影响其它字段。
func (a *Admin) responseOf(ctx context.Context, requestID int64) map[string]any {
	row, err := a.store.ResponseBody(ctx, requestID)
	if err != nil {
		return map[string]any{"recorded": false}
	}
	raw, err := trace.Decompress(row.BodyGzip)
	if err != nil {
		return map[string]any{"recorded": false}
	}
	out := parseResponse(raw)
	out["recorded"] = true
	out["truncated"] = row.Truncated != 0
	out["complete"] = row.Complete != 0
	return out
}

// parseResponse 兼容普通 JSON 与 SSE 两种响应体。
func parseResponse(raw []byte) map[string]any {
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "event:") || strings.Contains(text, "\ndata:") || strings.HasPrefix(text, "data:") {
		return parseSSE(text)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		return map[string]any{"text": text}
	}
	if content, ok := body["content"].([]any); ok {
		return anthropicMessage(content, body)
	}
	if choices, ok := body["choices"].([]any); ok && len(choices) > 0 {
		return openAIMessage(choices)
	}
	return map[string]any{"text": text}
}

// anthropicMessage 取 Anthropic 非流式响应里的文本、思考与工具名。
func anthropicMessage(content []any, body map[string]any) map[string]any {
	var text, thinking strings.Builder
	var tools []string
	blocks := make([]map[string]any, 0, len(content))
	for _, item := range content {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch block["type"] {
		case "text":
			text.WriteString(stringFieldValue(block["text"]))
			blocks = append(blocks, map[string]any{"type": "text", "text": stringFieldValue(block["text"])})
		case "thinking":
			thinking.WriteString(stringFieldValue(block["thinking"]))
			blocks = append(blocks, map[string]any{"type": "thinking", "thinking": stringFieldValue(block["thinking"])})
		case "tool_use":
			name := stringFieldValue(block["name"])
			tools = append(tools, name)
			blocks = append(blocks, map[string]any{"type": "tool_use", "name": name, "input": block["input"]})
		}
	}
	usage, _ := body["usage"].(map[string]any)
	return map[string]any{
		"text": text.String(), "thinking": thinking.String(), "tool_uses": tools,
		"blocks":      dropEmptyBlocks(blocks),
		"stop_reason": stringFieldValue(body["stop_reason"]),
		"usage_in":    numberField(usage, "input_tokens"),
		"usage_out":   numberField(usage, "output_tokens"),
		"usage_cache": numberField(usage, "cache_read_input_tokens"),
	}
}

// openAIMessage 取 OpenAI Chat 非流式响应里的文本与工具名。
func openAIMessage(choices []any) map[string]any {
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	var text strings.Builder
	text.WriteString(stringFieldValue(message["content"]))
	var tools []string
	if calls, ok := message["tool_calls"].([]any); ok {
		for _, call := range calls {
			if m, ok := call.(map[string]any); ok {
				if fn, ok := m["function"].(map[string]any); ok {
					tools = append(tools, stringFieldValue(fn["name"]))
				}
			}
		}
	}
	return map[string]any{
		"text": text.String(), "thinking": "", "tool_uses": tools,
		"stop_reason": stringFieldValue(choice["finish_reason"]),
	}
}

// parseSSE 从流式事件里累积文本、思考与工具名。
func parseSSE(raw string) map[string]any {
	var text, thinking strings.Builder
	var tools []string
	blocks := make([]map[string]any, 0)
	stopReason := ""
	var usageIn, usageOut, usageCache int64
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}
		switch stringFieldValue(event["type"]) {
		case "content_block_delta":
			delta, _ := event["delta"].(map[string]any)
			switch stringFieldValue(delta["type"]) {
			case "text_delta":
				piece := stringFieldValue(delta["text"])
				text.WriteString(piece)
				appendBlockText(&blocks, "text", "text", piece)
			case "thinking_delta":
				piece := stringFieldValue(delta["thinking"])
				thinking.WriteString(piece)
				appendBlockText(&blocks, "thinking", "thinking", piece)
			case "input_json_delta":
				appendToolInputJSON(&blocks, stringFieldValue(delta["partial_json"]))
			}
		case "content_block_start":
			block, _ := event["content_block"].(map[string]any)
			kind := stringFieldValue(block["type"])
			switch kind {
			case "tool_use":
				name := stringFieldValue(block["name"])
				tools = append(tools, name)
				blocks = append(blocks, map[string]any{"type": "tool_use", "name": name, "input": block["input"]})
			case "text":
				blocks = append(blocks, map[string]any{"type": "text", "text": ""})
			case "thinking":
				blocks = append(blocks, map[string]any{"type": "thinking", "thinking": ""})
			}
		case "message_start":
			message, _ := event["message"].(map[string]any)
			usage, _ := message["usage"].(map[string]any)
			usageIn, usageOut, usageCache = numberField(usage, "input_tokens"), numberField(usage, "output_tokens"), numberField(usage, "cache_read_input_tokens")
		case "message_delta":
			delta, _ := event["delta"].(map[string]any)
			stopReason = stringFieldValue(delta["stop_reason"])
			usage, _ := event["usage"].(map[string]any)
			if v := numberField(usage, "output_tokens"); v > 0 {
				usageOut = v
			}
		}
		if choices, ok := event["choices"].([]any); ok && len(choices) > 0 {
			choice, _ := choices[0].(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			text.WriteString(stringFieldValue(delta["content"]))
			if calls, ok := delta["tool_calls"].([]any); ok {
				for _, call := range calls {
					if m, ok := call.(map[string]any); ok {
						if fn, ok := m["function"].(map[string]any); ok {
							tools = append(tools, stringFieldValue(fn["name"]))
						}
					}
				}
			}
			if v := stringFieldValue(choice["finish_reason"]); v != "" {
				stopReason = v
			}
		}
	}
	finalizeToolInputs(blocks)
	blocks = dropEmptyBlocks(blocks)
	return map[string]any{
		"text": text.String(), "thinking": thinking.String(), "tool_uses": tools,
		"blocks":      blocks,
		"stop_reason": stopReason,
		"usage_in":    usageIn, "usage_out": usageOut, "usage_cache": usageCache,
	}
}

// stringFieldValue 读取字符串字段,缺失时返回空串。
func stringFieldValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// numberField 读取数值字段,缺失时返回 0。
func numberField(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}

// appendBlockText 把流式片段追加到最后一个同类型块;没有块时补一个。
func appendBlockText(blocks *[]map[string]any, kind, field, piece string) {
	for i := len(*blocks) - 1; i >= 0; i-- {
		block := (*blocks)[i]
		if stringFieldValue(block["type"]) != kind {
			continue
		}
		block[field] = stringFieldValue(block[field]) + piece
		return
	}
	*blocks = append(*blocks, map[string]any{"type": kind, field: piece})
}

// appendToolInputJSON 把流式工具入参片段累加到最后一个 tool_use 块。
func appendToolInputJSON(blocks *[]map[string]any, piece string) {
	for i := len(*blocks) - 1; i >= 0; i-- {
		block := (*blocks)[i]
		if stringFieldValue(block["type"]) != "tool_use" {
			continue
		}
		block["input_json"] = stringFieldValue(block["input_json"]) + piece
		return
	}
}

// finalizeToolInputs 把累积的 JSON 文本解析成对象;解析失败时保留原文。
func finalizeToolInputs(blocks []map[string]any) {
	for _, block := range blocks {
		raw := stringFieldValue(block["input_json"])
		delete(block, "input_json")
		if raw == "" {
			continue
		}
		var input any
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			block["input"] = raw
			continue
		}
		block["input"] = input
	}
}

// dropEmptyBlocks 移除内容为空的思考与文本块。
// 流式响应会先声明一个 thinking 块,再只发签名而不发正文;这种块没有展示价值。
func dropEmptyBlocks(blocks []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch stringFieldValue(block["type"]) {
		case "thinking":
			if strings.TrimSpace(stringFieldValue(block["thinking"])) == "" {
				continue
			}
		case "text":
			if strings.TrimSpace(stringFieldValue(block["text"])) == "" {
				continue
			}
		}
		out = append(out, block)
	}
	return out
}
