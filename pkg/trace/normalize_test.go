package trace

import (
	"encoding/json"
	"strings"
	"testing"
)

// skeleton 返回消息序列的骨架(角色 + 块类型),用于跨协议比对。
func skeleton(msgs []MainlineMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		types := make([]string, 0, len(m.Blocks))
		for _, b := range m.Blocks {
			types = append(types, typeOf(map[string]any(b)))
		}
		out = append(out, m.Role+"("+strings.Join(types, ",")+")")
	}
	return out
}

// blockOf 取第 msgIdx 条消息的第 blockIdx 个块。
func blockOf(t *testing.T, msgs []MainlineMessage, msgIdx, blockIdx int) MainlineBlock {
	t.Helper()
	if msgIdx >= len(msgs) {
		t.Fatalf("消息数 = %d, want > %d", len(msgs), msgIdx)
	}
	if blockIdx >= len(msgs[msgIdx].Blocks) {
		t.Fatalf("第 %d 条消息的块数 = %d, want > %d", msgIdx, len(msgs[msgIdx].Blocks), blockIdx)
	}
	return msgs[msgIdx].Blocks[blockIdx]
}

const anthropicSample = `{
  "model": "claude-sonnet-4-5",
  "system": "You are helpful.",
  "messages": [
    {"role": "user", "content": "hi"},
    {"role": "assistant", "content": [
      {"type": "thinking", "thinking": "let me think", "signature": "sig-abc"},
      {"type": "text", "text": "hello"},
      {"type": "tool_use", "id": "toolu_1", "name": "calc", "input": {"n": 42}}
    ]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "toolu_1", "content": "42",
       "is_error": false, "cache_control": {"type": "ephemeral"}}
    ]}
  ]
}`

const openaiSample = `{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "You are helpful."},
    {"role": "user", "content": "hi"},
    {"role": "assistant", "content": "hello", "reasoning_content": "let me think",
     "tool_calls": [{"id": "call_1", "type": "function",
       "function": {"name": "calc", "arguments": "{\"n\":42}"}}]},
    {"role": "tool", "tool_call_id": "call_1", "content": "42"}
  ]
}`

// TestNormalizeAnthropicBasicAndFidelity 覆盖 Anthropic 主干映射与保真:
// system 作首条伪消息、content 字符串补成 text 块、thinking/signature/cache_control 保留。
func TestNormalizeAnthropicBasicAndFidelity(t *testing.T) {
	msgs, err := Normalize("anthropic-messages", []byte(anthropicSample))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	want := []string{"system(text)", "user(text)", "assistant(thinking,text,tool_use)", "user(tool_result)"}
	got := skeleton(msgs)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("骨架 = %v, want %v", got, want)
	}

	// thinking 家族归一为 thinking,但 signature 原样保留。
	thinking := blockOf(t, msgs, 2, 0)
	if thinking["thinking"] != "let me think" || thinking["signature"] != "sig-abc" {
		t.Errorf("thinking 块丢字段: %+v", thinking)
	}
	// 注解字段保留。
	result := blockOf(t, msgs, 3, 0)
	if result["is_error"] != false {
		t.Errorf("is_error = %v, want false", result["is_error"])
	}
	if _, ok := result["cache_control"]; !ok {
		t.Errorf("cache_control 被丢弃: %+v", result)
	}
	// 数字以 json.Number 保留,不因浮点转换失真。
	input, ok := blockOf(t, msgs, 2, 2)["input"].(map[string]any)
	if !ok {
		t.Fatalf("tool_use.input 类型 = %T", blockOf(t, msgs, 2, 2)["input"])
	}
	if n, isNum := input["n"].(json.Number); !isNum || n.String() != "42" {
		t.Errorf("input.n = %#v, want json.Number(42)", input["n"])
	}
}

// TestNormalizeOpenAIBasicAndFidelity 覆盖 OpenAI 主干映射:
// reasoning_content 提升为 thinking、tool_calls 展开为 tool_use、role=tool 归为 tool_result。
func TestNormalizeOpenAIBasicAndFidelity(t *testing.T) {
	msgs, err := Normalize("openai-chat", []byte(openaiSample))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	// OpenAI 的 role=tool 归一为 user:与 Anthropic 的 tool_result 块对齐。
	want := []string{"system(text)", "user(text)", "assistant(thinking,text,tool_use)", "user(tool_result)"}
	got := skeleton(msgs)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("骨架 = %v, want %v", got, want)
	}

	call := blockOf(t, msgs, 2, 2)
	if call["id"] != "call_1" || call["name"] != "calc" {
		t.Errorf("tool_use 标识/名称 = %+v", call)
	}
	input, ok := call["input"].(map[string]any)
	if !ok {
		t.Fatalf("input 类型 = %T(应已解析 arguments)", call["input"])
	}
	if n, isNum := input["n"].(json.Number); !isNum || n.String() != "42" {
		t.Errorf("input.n = %#v, want json.Number(42)", input["n"])
	}

	result := blockOf(t, msgs, 3, 0)
	if result["tool_use_id"] != "call_1" {
		t.Errorf("tool_result.tool_use_id = %v, want call_1", result["tool_use_id"])
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("tool_result.content = %#v", result["content"])
	}
	if part, _ := content[0].(map[string]any); part["text"] != "42" {
		t.Errorf("tool_result 内容 = %#v", content[0])
	}
}

// TestNormalizeCrossProtocolIsomorphic 覆盖跨协议同构:语义等价的两组输入
// 生成相同的主干骨架(角色与块类型序列一致)。
func TestNormalizeCrossProtocolIsomorphic(t *testing.T) {
	anth, err := Normalize("anthropic-messages", []byte(anthropicSample))
	if err != nil {
		t.Fatalf("anthropic Normalize() error = %v", err)
	}
	oa, err := Normalize("openai-chat", []byte(openaiSample))
	if err != nil {
		t.Fatalf("openai Normalize() error = %v", err)
	}
	if strings.Join(skeleton(anth), "|") != strings.Join(skeleton(oa), "|") {
		t.Fatalf("跨协议骨架不同:\n anthropic: %v\n openai:   %v", skeleton(anth), skeleton(oa))
	}
}

// TestNormalizeKeepsUnknownBlocks 覆盖未知块:类型与载荷原样保留。
func TestNormalizeKeepsUnknownBlocks(t *testing.T) {
	body := `{"messages":[{"role":"user","content":[{"type":"custom_thing","payload":{"a":1}}]}]}`
	msgs, err := Normalize("anthropic-messages", []byte(body))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	block := blockOf(t, msgs, 0, 0)
	if block["type"] != "custom_thing" {
		t.Errorf("type = %v, want custom_thing", block["type"])
	}
	if _, ok := block["payload"]; !ok {
		t.Errorf("未知块的载荷被丢弃: %+v", block)
	}
}

// TestNormalizeBlobRefs 覆盖二进制转引用:字节本体不进入快照。
func TestNormalizeBlobRefs(t *testing.T) {
	const payload = "iVBORw0KGgo=" // 8 字节的有效 base64
	cases := []struct {
		name     string
		protocol string
		body     string
		wantType string
	}{
		{
			"anthropic base64 图片",
			"anthropic-messages",
			`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + payload + `"}}]}]}`,
			"blob",
		},
		{
			"openai data URL 图片",
			"openai-chat",
			`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` + payload + `"}}]}]}`,
			"blob",
		},
		{
			"anthropic data URL(置于 url 字段)",
			"anthropic-messages",
			`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","media_type":"image/png","url":"data:image/png;base64,` + payload + `"}}]}]}`,
			"blob",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgs, err := Normalize(tc.protocol, []byte(tc.body))
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			block := blockOf(t, msgs, 0, 0)
			if block["type"] != tc.wantType {
				t.Fatalf("type = %v, want %v", block["type"], tc.wantType)
			}
			if block["bytes"] != 8 {
				t.Errorf("bytes = %v, want 8", block["bytes"])
			}
			if hash, _ := block["hash"].(string); len(hash) != 64 {
				t.Errorf("hash = %q, want 64 位十六进制", hash)
			}
			if _, leaked := block["data"]; leaked {
				t.Errorf("字节本体进入快照: %+v", block)
			}
			if _, leaked := block["source"]; leaked {
				t.Errorf("原始 source 进入快照: %+v", block)
			}
		})
	}
}

// TestNormalizeBlobKeepsURL 覆盖 URL 形态:远程图片保留地址,不下载。
func TestNormalizeBlobKeepsURL(t *testing.T) {
	body := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`
	msgs, err := Normalize("openai-chat", []byte(body))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	block := blockOf(t, msgs, 0, 0)
	if block["type"] != "blob" || block["url"] != "https://example.com/a.png" {
		t.Errorf("URL 形态 = %+v", block)
	}
}

// TestNormalizeInvalidArgumentsKept 覆盖非法入参:解析失败时保留原样字符串。
func TestNormalizeInvalidArgumentsKept(t *testing.T) {
	body := `{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","function":{"name":"f","arguments":"{not json"}}]}]}`
	msgs, err := Normalize("openai-chat", []byte(body))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	input := blockOf(t, msgs, 0, 0)["input"]
	if s, ok := input.(string); !ok || s != "{not json" {
		t.Errorf("input = %#v, want 原样字符串", input)
	}
}

// TestNormalizeEmptyAndError 覆盖空消息与错误分支。
func TestNormalizeEmptyAndError(t *testing.T) {
	if msgs, err := Normalize("anthropic-messages", []byte(`{}`)); err != nil || len(msgs) != 0 {
		t.Errorf("空请求体 = %v, %v; want 空", msgs, err)
	}
	if _, err := Normalize("anthropic-messages", []byte(`not-json`)); err == nil {
		t.Error("非法 JSON 未报错")
	}
	if _, err := Normalize("openai-responses", []byte(`{}`)); err == nil {
		t.Error("未支持协议未报错")
	}
}
