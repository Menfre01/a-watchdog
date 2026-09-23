package protocol

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ds-context/pkg/chain"
)

// capturedRequest 是假上游捕获的请求快照。
type capturedRequest struct {
	Path    string
	Query   string
	Headers http.Header
	Body    []byte
}

// newCaptureUpstream 启动假上游:捕获请求并返回指定状态 / 内容类型 / 响应体。
func newCaptureUpstream(t *testing.T, status int, contentType, respBody string) (*httptest.Server, chan capturedRequest) {
	t.Helper()
	captured := make(chan capturedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		captured <- capturedRequest{
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    body,
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		if _, err := io.WriteString(w, respBody); err != nil {
			t.Errorf("upstream write: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

// newProxyCtx 构造 Proxy.Handle 所需的请求上下文(Recorder 承接响应)。
func newProxyCtx(t *testing.T, protocolName, path, query, body string, route *chain.RouteDecision, headers map[string]string) (*chain.Context, *httptest.ResponseRecorder) {
	t.Helper()
	target := path
	if query != "" {
		target += "?" + query
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, target, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	return &chain.Context{
		W:         rec,
		R:         req,
		Protocol:  protocolName,
		Body:      []byte(body),
		Route:     route,
		StartedAt: time.Now(),
	}, rec
}

func TestProxy_OpenAIChatPassthrough(t *testing.T) {
	respBody := `{"id":"chatcmpl-1","usage":{"prompt_tokens":5,"completion_tokens":7},"choices":[{"message":{"role":"assistant","content":"hi"}}]}`
	upstream, captured := newCaptureUpstream(t, http.StatusOK, "application/json", respBody)

	route := &chain.RouteDecision{
		ClientModel:      "alias",
		ProviderID:       1,
		ProviderName:     "deepseek",
		ProviderProtocol: "openai-chat",
		BaseURL:          upstream.URL,
		APIKey:           "sk-upstream",
		UpstreamModel:    "up-model",
	}
	reqBody := `{"model":"alias","stream":false,"messages":[{"role":"user","content":"hi"}]}`
	ctx, rec := newProxyCtx(t, "openai-chat", "/v1/chat/completions", "", reqBody, route, map[string]string{
		"Authorization": "Bearer client-key-should-not-leak",
		"X-Custom":      "secret",
	})

	if err := NewProxyWithClient(upstream.Client()).Handle(ctx); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	capReq := <-captured
	if capReq.Path != "/v1/chat/completions" {
		t.Fatalf("upstream path = %q", capReq.Path)
	}
	if got := capReq.Headers.Get("Authorization"); got != "Bearer sk-upstream" {
		t.Fatalf("upstream Authorization = %q, want provider credential", got)
	}
	if capReq.Headers.Get("X-Custom") != "" {
		t.Fatalf("X-Custom 不应出站")
	}
	if got := capReq.Headers.Get("User-Agent"); got != "ds-context" {
		t.Fatalf("User-Agent = %q, want ds-context", got)
	}
	var sent map[string]any
	if err := json.Unmarshal(capReq.Body, &sent); err != nil {
		t.Fatalf("upstream body invalid: %v", err)
	}
	if sent["model"] != "up-model" {
		t.Fatalf("upstream model = %v, want up-model", sent["model"])
	}
	if len(sent["messages"].([]any)) != 1 {
		t.Fatalf("messages 应原样保留: %v", sent["messages"])
	}

	// 响应原样回传 + 元数据落上下文。
	if rec.Code != http.StatusOK || rec.Body.String() != respBody {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
	if ctx.Upstream == nil || ctx.Upstream.StatusCode != http.StatusOK || ctx.Upstream.Stream {
		t.Fatalf("outcome = %+v", ctx.Upstream)
	}
	if ctx.Upstream.UsageIn == nil || *ctx.Upstream.UsageIn != 5 || ctx.Upstream.UsageOut == nil || *ctx.Upstream.UsageOut != 7 {
		t.Fatalf("usage = %+v", ctx.Upstream)
	}
	if !ctx.ResponseStarted {
		t.Fatalf("ResponseStarted = false, want true")
	}
}

func TestProxy_AnthropicStreamingPassthrough(t *testing.T) {
	sse := "event: message_start\n" +
		`data: {"type":"message_start","message":{"usage":{"input_tokens":100,"output_tokens":1}}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":55}}` + "\n\n"
	upstream, captured := newCaptureUpstream(t, http.StatusOK, "text/event-stream", sse)

	route := &chain.RouteDecision{
		ClientModel:      "claude-sonnet-4-5",
		ProviderProtocol: "anthropic-messages",
		BaseURL:          upstream.URL,
		APIKey:           "sk-ant-up",
		UpstreamModel:    "claude-sonnet-4-5",
	}
	reqBody := `{"model":"claude-sonnet-4-5","stream":true,"messages":[]}`
	ctx, rec := newProxyCtx(t, "anthropic-messages", "/v1/messages", "beta=true", reqBody, route, map[string]string{
		"X-Api-Key":      "client-key-should-not-leak",
		"Anthropic-Beta": "oauth-2025-04-20",
	})

	if err := NewProxyWithClient(upstream.Client()).Handle(ctx); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	capReq := <-captured
	if capReq.Path != "/v1/messages" || capReq.Query != "beta=true" {
		t.Fatalf("upstream target = %q?%q", capReq.Path, capReq.Query)
	}
	if got := capReq.Headers.Get("X-Api-Key"); got != "sk-ant-up" {
		t.Fatalf("客户端 key 不应出站, got %q", got)
	}
	if got := capReq.Headers.Get("Anthropic-Version"); got != defaultAnthropicVersion {
		t.Fatalf("Anthropic-Version = %q, want default", got)
	}
	if got := capReq.Headers.Get("Anthropic-Beta"); got != "oauth-2025-04-20" {
		t.Fatalf("Anthropic-Beta = %q, want passthrough", got)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("response code = %d", rec.Code)
	}
	if rec.Body.String() != sse {
		t.Fatalf("SSE 应逐字节透传, got %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if ctx.Upstream == nil || !ctx.Upstream.Stream {
		t.Fatalf("outcome = %+v", ctx.Upstream)
	}
	if ctx.Upstream.UsageIn == nil || *ctx.Upstream.UsageIn != 100 ||
		ctx.Upstream.UsageOut == nil || *ctx.Upstream.UsageOut != 55 {
		t.Fatalf("usage = %+v", ctx.Upstream)
	}
}

func TestProxy_UpstreamErrorPassthrough(t *testing.T) {
	errBody := `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`
	upstream, _ := newCaptureUpstream(t, http.StatusBadRequest, "application/json", errBody)

	route := &chain.RouteDecision{
		ProviderProtocol: "anthropic-messages",
		BaseURL:          upstream.URL,
		APIKey:           "sk-ant-up",
		UpstreamModel:    "m",
	}
	ctx, rec := newProxyCtx(t, "anthropic-messages", "/v1/messages", "", `{"model":"m","messages":[]}`, route, nil)

	if err := NewProxyWithClient(upstream.Client()).Handle(ctx); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	// 上游错误体逐字节透传,不包裹。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if rec.Body.String() != errBody {
		t.Fatalf("error body = %q, want upstream bytes", rec.Body.String())
	}
}

func TestProxy_UpstreamUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // 立即关闭:模拟上游不可达

	route := &chain.RouteDecision{
		ProviderProtocol: "openai-chat",
		BaseURL:          url,
		APIKey:           "sk",
		UpstreamModel:    "m",
	}
	ctx, rec := newProxyCtx(t, "openai-chat", "/v1/chat/completions", "", `{"model":"m"}`, route, nil)

	err := NewProxyWithClient(&http.Client{Timeout: 3 * time.Second}).Handle(ctx)
	if err == nil {
		t.Fatalf("Handle() error = nil, want upstream failure")
	}
	if ctx.ResponseStarted || rec.Body.Len() != 0 {
		t.Fatalf("上游不可达时不应写出响应(由链路统一处理): %v", rec.Body.String())
	}
}

func TestProxy_InvalidJSONBody(t *testing.T) {
	route := &chain.RouteDecision{
		ProviderProtocol: "openai-chat",
		BaseURL:          "http://127.0.0.1:1",
		APIKey:           "sk",
		UpstreamModel:    "m",
	}
	ctx, _ := newProxyCtx(t, "openai-chat", "/v1/chat/completions", "", `not-json`, route, nil)

	err := NewProxy().Handle(ctx)
	if err == nil || !strings.Contains(err.Error(), "build upstream request") {
		t.Fatalf("Handle() error = %v, want build failure", err)
	}
}

func TestProxy_DoesNotFollowRedirect(t *testing.T) {
	// 重定向目标若被访问即视为凭据外泄路径。
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("重定向目标不应被访问(检测到凭据外泄路径), UA=%q", r.Header.Get("User-Agent"))
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
	}))
	defer redirector.Close()

	route := &chain.RouteDecision{
		ProviderProtocol: "anthropic-messages",
		BaseURL:          redirector.URL,
		APIKey:           "sk-secret",
		UpstreamModel:    "m",
	}
	ctx, rec := newProxyCtx(t, "anthropic-messages", "/v1/messages", "", `{"model":"m"}`, route, nil)

	// 用默认构造的 Proxy(默认客户端必须自带禁止重定向策略)。
	if err := NewProxy().Handle(ctx); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 原样透传", rec.Code)
	}
}
