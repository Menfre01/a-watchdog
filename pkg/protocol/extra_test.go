package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"awatchdog/pkg/chain"
)

func TestAnthropicErrorResponse_AllStatuses(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusForbidden, "permission_error"},
		{http.StatusTooManyRequests, "rate_limit_error"},
		{http.StatusInternalServerError, "api_error"},
		{http.StatusBadRequest, "invalid_request_error"},
	}
	for _, c := range cases {
		_, body := (anthropicAdapter{}).ErrorResponse(c.status, "k", "msg")
		var doc struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatalf("status %d: bad body %s", c.status, body)
		}
		if doc.Error.Type != c.want {
			t.Fatalf("status %d: type = %q, want %q", c.status, doc.Error.Type, c.want)
		}
	}
}

func TestOpenAI_BuildUpstream_DefaultContentType(t *testing.T) {
	in := UpstreamInput{
		InboundPath: "/v1/chat/completions",
		Header:      http.Header{},
		Body:        []byte(`{"model":"m"}`),
		Provider:    ProviderTarget{BaseURL: "https://x.example.com", APIKey: "k", Model: "mm"},
	}
	req, err := (openaiChatAdapter{}).BuildUpstream(context.Background(), in)
	if err != nil {
		t.Fatalf("BuildUpstream() error = %v", err)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want default", got)
	}
}

func TestBuildUpstream_InvalidJSON(t *testing.T) {
	in := UpstreamInput{Body: []byte("not-json"), Provider: ProviderTarget{Model: "m", BaseURL: "https://x"}}
	if _, err := (openaiChatAdapter{}).BuildUpstream(context.Background(), in); err == nil {
		t.Fatalf("openai BuildUpstream(bad json) error = nil")
	}
	if _, err := (anthropicAdapter{}).BuildUpstream(context.Background(), in); err == nil {
		t.Fatalf("anthropic BuildUpstream(bad json) error = nil")
	}
	if _, err := rewriteModel([]byte("{bad"), "m"); err == nil {
		t.Fatalf("rewriteModel(bad json) error = nil")
	}
}

func TestJSONUsageParser_OversizeStopsParsing(t *testing.T) {
	p := &jsonUsageParser{}
	p.Write(make([]byte, maxJSONBodyUsage+1))
	// 到达上限后应放弃解析,后续写入被忽略、用量保持为空。
	p.Write([]byte(`{"usage":{"prompt_tokens":1}}`))
	if u := p.Usage(); u.In != nil || u.Out != nil || u.Cache != nil {
		t.Fatalf("oversize usage = %+v, want empty", u)
	}
}

func TestJSONUsageParser_ChunkedWriteAndCache(t *testing.T) {
	p := &jsonUsageParser{}
	p.Write([]byte(`{"usage":{"prompt_`))
	p.Write([]byte(`tokens":11,"completion_tokens":22}}`))
	u := p.Usage()
	if u.In == nil || *u.In != 11 || u.Out == nil || *u.Out != 22 {
		t.Fatalf("chunked usage = %+v", u)
	}
	// 二次读取应稳定(解析一次并缓存)。
	if u2 := p.Usage(); u2.In == nil || *u2.In != 11 {
		t.Fatalf("cached usage = %+v", u2)
	}
}

func TestSSEParser_UnterminatedLineNotParsed(t *testing.T) {
	obs := NewObserver("openai-chat", "text/event-stream")
	// 未以换行结尾的数据不应被解析(仍可能有后续分块)。
	obs.Write([]byte(`data: {"usage":{"prompt_tokens":1,"completion_tokens":2}}`))
	if obs.Usage().In != nil {
		t.Fatalf("未终结的行不应被解析")
	}
}

func TestProxy_HandleMissingRoute(t *testing.T) {
	ctx := &chain.Context{
		W: httptest.NewRecorder(),
		R: httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil),
	}
	err := NewProxy().Handle(ctx)
	if err == nil || !strings.Contains(err.Error(), "missing route") {
		t.Fatalf("Handle() error = %v, want missing route", err)
	}
}

// failingReader 先给一段数据,再返回错误。
type failingReader struct {
	done bool
}

// Read 实现 io.Reader。
func (r *failingReader) Read(b []byte) (int, error) {
	if !r.done {
		r.done = true
		return copy(b, "xx"), nil
	}
	return 0, errors.New("read boom")
}

// failingWriter 是写入必失败的 ResponseWriter。
type failingWriter struct{}

// Header 实现 http.ResponseWriter。
func (failingWriter) Header() http.Header { return http.Header{} }

// Write 实现 http.ResponseWriter。
func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write boom") }

// WriteHeader 实现 http.ResponseWriter。
func (failingWriter) WriteHeader(int) {}

func TestRelayStream_ErrorPaths(t *testing.T) {
	// 上游读取错误。
	rec := httptest.NewRecorder()
	if err := RelayStream(rec, &failingReader{}, &Observer{}); err == nil {
		t.Fatalf("RelayStream(read err) error = nil")
	}
	// 客户端写入错误。
	if err := RelayStream(failingWriter{}, strings.NewReader("xx"), &Observer{}); err == nil {
		t.Fatalf("RelayStream(write err) error = nil")
	}
	// 立即 EOF:正常返回。
	if err := RelayStream(httptest.NewRecorder(), strings.NewReader(""), &Observer{}); err != nil {
		t.Fatalf("RelayStream(empty) error = %v", err)
	}
}

func TestParseJSONUsage_NoUsage(t *testing.T) {
	u := &Usage{}
	parseJSONUsage([]byte(`{"id":"x"}`), u)
	if u.In != nil || u.Out != nil || u.Cache != nil {
		t.Fatalf("no-usage parse = %+v", u)
	}
	// OpenAI 的 cached_tokens 分支。
	parseJSONUsage([]byte(`{"usage":{"prompt_tokens":5,"completion_tokens":6,"prompt_tokens_details":{"cached_tokens":2}}}`), u)
	if u.In == nil || *u.In != 5 || u.Cache == nil || *u.Cache != 2 {
		t.Fatalf("cached parse = %+v", u)
	}
}

func TestStreamMalfunction(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// 声明并写出一部分后中断,模拟上游半途断开。
		_, _ = io.WriteString(w, "data: partial\n\n")
	}))
	defer s.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, s.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	rec := httptest.NewRecorder()
	if err := RelayStream(rec, resp.Body, NewObserver("openai-chat", "text/event-stream")); err != nil {
		t.Fatalf("RelayStream() error = %v", err)
	}
}
