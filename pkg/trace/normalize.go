package trace

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// MainlineMessage 是主干消息:一行一条 canonical JSON。
// 消息保留原始内容(最大保真),仅类型名与跨协议形态被归一(design.md D3)。
type MainlineMessage struct {
	Role   string          `json:"role"`
	Blocks []MainlineBlock `json:"blocks"`
}

// MainlineBlock 是主干块:保留原始 JSON 的全部键,只按类型映射表归一 type 名。
type MainlineBlock map[string]any

// Normalize 按协议把入向请求体规范化为主干消息序列。
// 请求体不可解析时返回错误(调用方据此只落原始层)。
func Normalize(protocolName string, body []byte) ([]MainlineMessage, error) {
	switch protocolName {
	case "anthropic-messages":
		return normalizeAnthropic(body)
	case "openai-chat":
		return normalizeOpenAI(body)
	default:
		return nil, fmt.Errorf("trace: 未支持的入向协议 %q", protocolName)
	}
}

// ---- Anthropic Messages ----

// anthropicDoc 是 Anthropic 请求体中规范化所需的部分。
type anthropicDoc struct {
	System   json.RawMessage `json:"system"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

// normalizeAnthropic 把 Anthropic 请求体规范化为主干:system 作为首条伪消息,
// messages 逐条打平,content 的字符串快捷形式补成 text 块。
func normalizeAnthropic(body []byte) ([]MainlineMessage, error) {
	var doc anthropicDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("trace: 解析 anthropic 请求体: %w", err)
	}
	out := make([]MainlineMessage, 0, len(doc.Messages)+1)
	if blocks, err := anthropicBlocks(doc.System); err != nil {
		return nil, err
	} else if len(blocks) > 0 {
		out = append(out, MainlineMessage{Role: "system", Blocks: blocks})
	}
	for _, m := range doc.Messages {
		blocks, err := anthropicBlocks(m.Content)
		if err != nil {
			return nil, err
		}
		out = append(out, MainlineMessage{Role: m.Role, Blocks: blocks})
	}
	return out, nil
}

// anthropicBlocks 把 content(字符串或块数组)归一为块序列。
func anthropicBlocks(content json.RawMessage) ([]MainlineBlock, error) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, fmt.Errorf("trace: 解析 anthropic content 字符串: %w", err)
		}
		return []MainlineBlock{{"type": "text", "text": text}}, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(trimmed, &raws); err != nil {
		return nil, fmt.Errorf("trace: 解析 anthropic content 数组: %w", err)
	}
	blocks := make([]MainlineBlock, 0, len(raws))
	for _, raw := range raws {
		obj, err := decodeObject(raw)
		if err != nil {
			return nil, fmt.Errorf("trace: 解析 anthropic 块: %w", err)
		}
		blocks = append(blocks, anthropicBlock(obj))
	}
	return blocks, nil
}

// anthropicBlock 归一单个 Anthropic 块:未知块原样保留。
func anthropicBlock(obj map[string]any) MainlineBlock {
	switch typeOf(obj) {
	case "thinking", "redacted_thinking":
		// thinking 家族统一为 thinking;signature 与 redacted 数据随原键保留。
		obj["type"] = "thinking"
		return MainlineBlock(obj)
	case "image", "document":
		return blobBlockFromAnthropic(obj)
	default:
		return MainlineBlock(obj)
	}
}

// blobBlockFromAnthropic 把 Anthropic 的 image/document 换成字节引用:
// base64 源取长度与哈希,url 源保留地址,字节本体一律不进入快照。
func blobBlockFromAnthropic(obj map[string]any) MainlineBlock {
	source, _ := obj["source"].(map[string]any)
	if source == nil {
		return MainlineBlock(obj)
	}
	if data, _ := source["data"].(string); data != "" {
		return byteRefBlock(data, stringField(source, "media_type"))
	}
	if url, _ := source["url"].(string); url != "" {
		// 部分实现把 data URL 放在 url 字段:与 OpenAI 侧同样转引用,避免字节本体入库。
		if payload, mediaType, ok := splitDataURL(url); ok {
			return byteRefBlock(payload, mediaType)
		}
		return MainlineBlock{"type": "blob", "url": url}
	}
	return MainlineBlock(obj)
}

// ---- OpenAI Chat Completions ----

// openaiDoc 是 OpenAI 请求体中规范化所需的部分。
type openaiDoc struct {
	Messages []openaiMessage `json:"messages"`
}

// openaiMessage 是 OpenAI Chat 的单条消息(规范化所需字段)。
type openaiMessage struct {
	Role             string           `json:"role"`
	Content          json.RawMessage  `json:"content"`
	ReasoningContent string           `json:"reasoning_content"`
	ToolCallID       string           `json:"tool_call_id"`
	ToolCalls        []openaiToolCall `json:"tool_calls"`
}

// openaiToolCall 是 OpenAI 的工具调用描述(arguments 为 JSON 字符串)。
type openaiToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// normalizeOpenAI 把 OpenAI Chat 请求体规范化为主干:
// tool_calls 展开为 tool_use 块,role=tool 归为 tool_result 块,
// reasoning_content 提升为 thinking 块(DeepSeek 风格的兼容入向)。
func normalizeOpenAI(body []byte) ([]MainlineMessage, error) {
	var doc openaiDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("trace: 解析 openai 请求体: %w", err)
	}
	out := make([]MainlineMessage, 0, len(doc.Messages))
	for _, m := range doc.Messages {
		blocks, err := openaiBlocks(m)
		if err != nil {
			return nil, err
		}
		role := m.Role
		// OpenAI 把工具结果放在 role=tool 的消息里,Anthropic 放在 role=user 的
		// tool_result 块里。主干按后者归一,使跨协议骨架可比(design.md D3)。
		if role == "tool" {
			role = "user"
		}
		out = append(out, MainlineMessage{Role: role, Blocks: blocks})
	}
	return out, nil
}

// openaiBlocks 把一条 OpenAI 消息归一为块序列:thinking → text → tool_use / tool_result。
func openaiBlocks(m openaiMessage) ([]MainlineBlock, error) {
	var blocks []MainlineBlock
	if m.ReasoningContent != "" {
		blocks = append(blocks, MainlineBlock{"type": "thinking", "thinking": m.ReasoningContent})
	}
	if m.Role == "tool" {
		result := MainlineBlock{"type": "tool_result", "tool_use_id": m.ToolCallID}
		if content, err := openaiContentBlocks(m.Content); err != nil {
			return nil, err
		} else if len(content) > 0 {
			result["content"] = anyBlocks(content)
		}
		blocks = append(blocks, result)
		return blocks, nil
	}
	content, err := openaiContentBlocks(m.Content)
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, content...)
	for _, call := range m.ToolCalls {
		blocks = append(blocks, MainlineBlock{
			"type":  "tool_use",
			"id":    call.ID,
			"name":  call.Function.Name,
			"input": decodeArguments(call.Function.Arguments),
		})
	}
	return blocks, nil
}

// openaiContentBlocks 归一 content(字符串或 parts 数组)。
func openaiContentBlocks(content json.RawMessage) ([]MainlineBlock, error) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, fmt.Errorf("trace: 解析 openai content 字符串: %w", err)
		}
		if text == "" {
			return nil, nil
		}
		return []MainlineBlock{{"type": "text", "text": text}}, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(trimmed, &raws); err != nil {
		return nil, fmt.Errorf("trace: 解析 openai content 数组: %w", err)
	}
	blocks := make([]MainlineBlock, 0, len(raws))
	for _, raw := range raws {
		obj, err := decodeObject(raw)
		if err != nil {
			return nil, fmt.Errorf("trace: 解析 openai part: %w", err)
		}
		blocks = append(blocks, openaiPartBlock(obj))
	}
	return blocks, nil
}

// openaiPartBlock 归一 content part:image_url 换成字节引用,未知 part 原样保留。
func openaiPartBlock(obj map[string]any) MainlineBlock {
	if typeOf(obj) != "image_url" {
		return MainlineBlock(obj)
	}
	image, _ := obj["image_url"].(map[string]any)
	url, _ := image["url"].(string)
	if url == "" {
		return MainlineBlock(obj)
	}
	if payload, mediaType, ok := splitDataURL(url); ok {
		return byteRefBlock(payload, mediaType)
	}
	return MainlineBlock{"type": "blob", "url": url}
}

// decodeArguments 解析 OpenAI 的工具入参 JSON 字符串;解析失败时保留原样字符串
// (保真优先:非法入参本身就是要观察的事实)。
func decodeArguments(arguments string) any {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return map[string]any{}
	}
	var value any
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return arguments
	}
	return value
}

// ---- 公共辅助 ----

// byteRefBlock 把 base64 载荷换成字节引用:只留哈希、媒体类型与长度。
func byteRefBlock(payload, mediaType string) MainlineBlock {
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			// 非法 base64:保留载荷长度描述,不保留本体。
			return MainlineBlock{"type": "blob", "media_type": mediaType, "bytes": -1}
		}
	}
	sum := sha256.Sum256(decoded)
	block := MainlineBlock{
		"type":  "blob",
		"hash":  hex.EncodeToString(sum[:]),
		"bytes": len(decoded),
	}
	if mediaType != "" {
		block["media_type"] = mediaType
	}
	return block
}

// splitDataURL 拆解 data URL(形如 data:image/png;base64,xxxx)。
func splitDataURL(url string) (payload, mediaType string, ok bool) {
	if !strings.HasPrefix(url, "data:") {
		return "", "", false
	}
	meta, payload, found := strings.Cut(strings.TrimPrefix(url, "data:"), ",")
	if !found || !strings.HasSuffix(meta, ";base64") {
		return "", "", false
	}
	return payload, strings.TrimSuffix(meta, ";base64"), true
}

// anyBlocks 把块序列转成可嵌入 map 的形态(tool_result 的 content 字段)。
func anyBlocks(blocks []MainlineBlock) []any {
	out := make([]any, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, map[string]any(b))
	}
	return out
}

// typeOf 取块的 type 字段。
func typeOf(obj map[string]any) string {
	t, _ := obj["type"].(string)
	return t
}

// stringField 取字符串字段。
func stringField(obj map[string]any, key string) string {
	v, _ := obj[key].(string)
	return v
}

// decodeObject 以 json.Number 解析单个 JSON 对象:数字保留原样,不因浮点转换失真。
func decodeObject(raw json.RawMessage) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	return obj, nil
}
