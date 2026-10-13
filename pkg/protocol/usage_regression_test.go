package protocol

import "testing"

// checkInt64 断言用量字段等于期望值。
func checkInt64(t *testing.T, label string, got *int64, want int64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %d", label, want)
	}
	if *got != want {
		t.Fatalf("%s = %d, want %d", label, *got, want)
	}
}

// checkNil 断言用量字段为空(值未知)。
func checkNil(t *testing.T, label string, got *int64) {
	t.Helper()
	if got != nil {
		t.Fatalf("%s = %d, want nil", label, *got)
	}
}

// TestRegression_AnthropicInterruptedStreamKeepsOutputUnknown 覆盖流中断时的输出量口径。
//
// 根因:Anthropic 在 message_start 的 usage 里固定给 output_tokens 占位值(1),
// 真实输出量只出现在 message_delta。旧实现照抄 message_start 的值,于是任何在
// message_delta 之前中断的流(客户端取消、上游断开)都会被记成输出 1 token。
func TestRegression_AnthropicInterruptedStreamKeepsOutputUnknown(t *testing.T) {
	stream := "event: message_start\n" +
		`data: {"type":"message_start","message":{"usage":{"input_tokens":100,"output_tokens":1}}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}` + "\n\n"

	obs := NewObserver("anthropic-messages", "text/event-stream")
	obs.Write([]byte(stream))
	u := obs.Usage()
	checkInt64(t, "In", u.In, 100)
	checkNil(t, "Out", u.Out)
}

// TestRegression_AnthropicPartialCacheUpdateKeepsOtherComponent 覆盖缓存分量被部分覆盖的丢数问题。
//
// 根因:旧实现把缓存读与缓存写求和后写进单列,两个分量无法独立更新。当
// message_delta 只带 cache_read_input_tokens 时,先到的 cache_creation_input_tokens
// 被整列覆盖,缓存写入量丢失。
func TestRegression_AnthropicPartialCacheUpdateKeepsOtherComponent(t *testing.T) {
	stream := "event: message_start\n" +
		`data: {"type":"message_start","message":{"usage":{"input_tokens":100,"output_tokens":1,"cache_read_input_tokens":10,"cache_creation_input_tokens":20}}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":50,"cache_read_input_tokens":10}}` + "\n\n"

	obs := NewObserver("anthropic-messages", "text/event-stream")
	obs.Write([]byte(stream))
	u := obs.Usage()
	checkInt64(t, "Out", u.Out, 50)
	checkInt64(t, "Cache", u.Cache, 30) // 读 10 + 写 20
}

// TestRegression_AnthropicCacheCreationDetailObject 覆盖新版明细字段。
//
// 根因:新版 API 用 cache_creation 对象按 TTL 给出缓存写入明细
// (ephemeral_5m_input_tokens / ephemeral_1h_input_tokens)。旧实现只读汇总数字键,
// 这类响应的缓存写入量被完全丢弃。
func TestRegression_AnthropicCacheCreationDetailObject(t *testing.T) {
	stream := "event: message_delta\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":50,"cache_read_input_tokens":10,"cache_creation":{"ephemeral_5m_input_tokens":15,"ephemeral_1h_input_tokens":5}}}` + "\n\n"

	obs := NewObserver("anthropic-messages", "text/event-stream")
	obs.Write([]byte(stream))
	u := obs.Usage()
	checkInt64(t, "Cache", u.Cache, 30) // 读 10 + 写 15 + 写 5
}

// TestRegression_AnthropicCacheCreationDetailObjectNonStream 覆盖非流式路径的同一明细字段。
func TestRegression_AnthropicCacheCreationDetailObjectNonStream(t *testing.T) {
	obs := NewObserver("anthropic-messages", "application/json")
	obs.Write([]byte(`{"usage":{"input_tokens":7,"output_tokens":9,"cache_read_input_tokens":1,` +
		`"cache_creation":{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":2}}}`))
	u := obs.Usage()
	checkInt64(t, "In", u.In, 7)
	checkInt64(t, "Out", u.Out, 9)
	checkInt64(t, "Cache", u.Cache, 6) // 读 1 + 写 3 + 写 2
}

// TestRegression_AnthropicCacheCreationNonObjectIgnored 覆盖字段类型不符时的隔离。
//
// 根因:实现方可能把缓存写入量误发在 cache_creation 键下(数字而非明细对象)。
// 若让类型不符向上冒泡,整个事件的 json.Unmarshal 会失败,连带丢掉同一事件里的
// output_tokens 等用量字段。单字段异常必须只影响该字段。
func TestRegression_AnthropicCacheCreationNonObjectIgnored(t *testing.T) {
	stream := "event: message_delta\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":50,"cache_read_input_tokens":10,"cache_creation":20}}` + "\n\n"

	obs := NewObserver("anthropic-messages", "text/event-stream")
	obs.Write([]byte(stream))
	u := obs.Usage()
	checkInt64(t, "Out", u.Out, 50)
	checkInt64(t, "Cache", u.Cache, 10)
}
