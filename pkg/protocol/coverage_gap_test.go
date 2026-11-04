package protocol

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"awatchdog/pkg/chain"
)

// TestMarshalOrFallback 覆盖序列化兜底:编码失败时返回占位体。
func TestMarshalOrFallback(t *testing.T) {
	if got := string(marshalOrFallback(make(chan int), "fallback")); got != "fallback" {
		t.Errorf("编码失败 = %q, want fallback", got)
	}
	if got := string(marshalOrFallback(map[string]string{"a": "b"}, "fallback")); got != `{"a":"b"}` {
		t.Errorf("编码成功 = %q", got)
	}
}

// stubTransport 返回固定响应,用于覆盖 Proxy 的头处理分支。
type stubTransport struct {
	resp *http.Response
}

// RoundTrip 实现 http.RoundTripper。
func (s stubTransport) RoundTrip(*http.Request) (*http.Response, error) { return s.resp, nil }

// TestProxy_SkipsTrailerHeader 覆盖逐跳头的丢弃:
// Trailer 经客户端栈转发会声明永远不会出现的 trailer。
func TestProxy_SkipsTrailerHeader(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"application/json"},
			"Trailer":      {"X-Checksum"},
			"X-Upstream":   {"v"},
		},
		Body: io.NopCloser(strings.NewReader("ok")),
	}
	client := &http.Client{Transport: stubTransport{resp: resp}}
	route := &chain.RouteDecision{
		ProviderProtocol: "openai-chat",
		BaseURL:          "http://upstream.example",
		APIKey:           "sk",
		UpstreamModel:    "m",
	}
	ctx, rec := newProxyCtx(t, "openai-chat", "/v1/chat/completions", "", `{"model":"m"}`, route, nil)

	if err := NewProxyWithClient(client).Handle(ctx); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := rec.Header().Get("Trailer"); got != "" {
		t.Errorf("Trailer 被转发: %q", got)
	}
	if got := rec.Header().Get("X-Upstream"); got != "v" {
		t.Errorf("普通响应头丢失: %q", got)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("response = %d %q", rec.Code, rec.Body.String())
	}
}

// TestObserver_CaptureLimit 覆盖响应副本上限:达到上限后停止追加并标记截断。
func TestObserver_CaptureLimit(t *testing.T) {
	obs := NewObserver("", "")
	obs.Write(make([]byte, captureLimit))
	if body, truncated := obs.Captured(); len(body) != captureLimit || truncated {
		t.Fatalf("上限整块 = %d 字节, truncated=%v", len(body), truncated)
	}
	obs.Write([]byte("tail"))
	body, truncated := obs.Captured()
	if len(body) != captureLimit || !truncated {
		t.Fatalf("超限后 = %d 字节, truncated=%v, want %d/true", len(body), truncated, captureLimit)
	}
}

// TestObserver_CapturePartialChunk 覆盖最后一块只装入剩余空间。
func TestObserver_CapturePartialChunk(t *testing.T) {
	obs := NewObserver("", "")
	obs.Write(make([]byte, captureLimit-1))
	obs.Write([]byte("abcd"))
	body, truncated := obs.Captured()
	if len(body) != captureLimit || !truncated {
		t.Fatalf("部分装入 = %d 字节, truncated=%v, want %d/true", len(body), truncated, captureLimit)
	}
	if !strings.HasSuffix(string(body), "a") {
		t.Errorf("只应装入首个字节, 尾部 = %q", body[len(body)-1:])
	}
}

// TestCacheCreationDetailUnmarshalJSON 覆盖明细字段的容错解码:
// 非对象与字段类型不符都只丢弃该字段,不牵连同一事件的其余用量。
func TestCacheCreationDetailUnmarshalJSON(t *testing.T) {
	var d cacheCreationDetail

	// 非对象:直接忽略。
	if err := json.Unmarshal([]byte(`20`), &d); err != nil {
		t.Fatalf("非对象解码 error = %v, want nil", err)
	}
	if d.total() != nil {
		t.Errorf("非对象应保持零值: %+v", d)
	}

	// 字段类型不符:解析失败被吞掉,返回值仍为 nil。
	if err := json.Unmarshal([]byte(`{"ephemeral_5m_input_tokens":"bad"}`), &d); err != nil {
		t.Fatalf("字段类型不符 error = %v, want nil", err)
	}

	// 合法对象:按 TTL 求和。
	if err := json.Unmarshal([]byte(`{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":4}`), &d); err != nil {
		t.Fatalf("合法对象 error = %v", err)
	}
	total := d.total()
	if total == nil || *total != 7 {
		t.Fatalf("total() = %v, want 7", total)
	}
}
