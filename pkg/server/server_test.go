package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ds-context/pkg/config"
	"ds-context/pkg/config/db"
)

// testClientKey 是端到端测试用的客户端密钥明文。
const testClientKey = "dsctx_e2e_test_key"

// discardLogger 返回丢弃输出的 slog。
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// eventually 轮询等待条件成立(异步记录的测试同步点)。
func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("条件在超时前未成立")
}

// newTestStack 组装完整栈:真实 Store + 完整责任链 + 指向假上游的 provider/模型与可用 key。
func newTestStack(t *testing.T, upstreamURL string) (*config.Store, http.Handler) {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("config.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	ctxbg := context.Background()
	p, err := store.CreateProvider(ctxbg, db.CreateProviderParams{
		Name: "up", Protocol: "anthropic-messages", BaseUrl: upstreamURL, ApiKey: "sk-upstream", Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	if _, err := store.CreateModel(ctxbg, db.CreateModelParams{
		Name: "claude-sonnet-4-5", ProviderID: p.ID, UpstreamModel: "claude-sonnet-4-5-v2", Enabled: 1,
	}); err != nil {
		t.Fatalf("CreateModel() error = %v", err)
	}
	if _, err := store.CreateClientKey(ctxbg, db.CreateClientKeyParams{
		Name: "cli", KeyHash: config.HashClientKey(testClientKey), KeyPrefix: "dsctx_", Enabled: 1,
	}); err != nil {
		t.Fatalf("CreateClientKey() error = %v", err)
	}

	wireCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	chainHandler, _, err := Wire(wireCtx, store, discardLogger())
	if err != nil {
		t.Fatalf("Wire() error = %v", err)
	}
	return store, New(chainHandler, discardLogger()).Handler()
}

// postJSON 通过测试客户端发起请求。
func postJSON(t *testing.T, client *http.Client, url, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// readBody 读取并关闭响应体。
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func TestEndpoint_Rejections(t *testing.T) {
	// 上游不可达不影响前置拒绝路径。
	_, handler := newTestStack(t, "http://127.0.0.1:1")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	auth := map[string]string{"Authorization": "Bearer " + testClientKey}

	// Anthropic:无凭据 → 401 + Anthropic 形状。
	resp := postJSON(t, srv.Client(), srv.URL+"/v1/messages", `{"model":"claude-sonnet-4-5"}`, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "authentication_error") {
		t.Fatalf("no-cred = %d %s", resp.StatusCode, body)
	}

	// OpenAI:无凭据 → 401 + OpenAI 形状。
	resp = postJSON(t, srv.Client(), srv.URL+"/v1/chat/completions", `{"model":"m"}`, nil)
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "invalid_request_error") {
		t.Fatalf("openai no-cred = %d %s", resp.StatusCode, body)
	}

	// 无效 key → 401。
	resp = postJSON(t, srv.Client(), srv.URL+"/v1/messages", `{"model":"claude-sonnet-4-5"}`,
		map[string]string{"Authorization": "Bearer wrong"})
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad-key = %d %s", resp.StatusCode, body)
	}

	// 未命中模型 → 404 + Anthropic 形状,且不发起上游调用(上游本就不可达)。
	resp = postJSON(t, srv.Client(), srv.URL+"/v1/messages", `{"model":"nope"}`, auth)
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "not_found_error") {
		t.Fatalf("miss = %d %s", resp.StatusCode, body)
	}

	// 非法请求体 → 400。
	resp = postJSON(t, srv.Client(), srv.URL+"/v1/messages", `not-json`, auth)
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad-body = %d %s", resp.StatusCode, body)
	}
}

func TestEndpoint_PassthroughStreamingAndRecord(t *testing.T) {
	// 假上游:捕获改写后的请求,返回带 usage 的 SSE 流。
	type upstreamReq struct {
		Path   string
		APIKey string
		Model  string
	}
	got := make(chan upstreamReq, 1)
	sse := "event: message_start\n" +
		`data: {"type":"message_start","message":{"usage":{"input_tokens":100,"output_tokens":1}}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":55}}` + "\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &parsed)
		got <- upstreamReq{Path: r.URL.Path, APIKey: r.Header.Get("X-Api-Key"), Model: parsed.Model}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, sse)
	}))
	defer upstream.Close()

	store, handler := newTestStack(t, upstream.URL)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp := postJSON(t, srv.Client(), srv.URL+"/v1/messages?beta=true",
		`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
		map[string]string{"Authorization": "Bearer " + testClientKey, "Anthropic-Beta": "oauth-2025-04-20"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	streamBody := readBody(t, resp)
	if streamBody != sse {
		t.Fatalf("SSE 应逐字节透传\ngot: %q", streamBody)
	}

	up := <-got
	if up.Path != "/v1/messages" || up.APIKey != "sk-upstream" || up.Model != "claude-sonnet-4-5-v2" {
		t.Fatalf("upstream request = %+v", up)
	}

	// 请求记录最终可见(异步写入)。
	eventually(t, func() bool {
		n, cerr := store.CountRequests(context.Background())
		return cerr == nil && n == 1
	})
	rows, err := store.ListRecentRequests(context.Background(), 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListRecentRequests() = %v, %v", rows, err)
	}
	r := rows[0]
	if r.StatusCode != 200 || r.Stream != 1 || r.ClientKeyName != "cli" ||
		r.RequestedModel != "claude-sonnet-4-5" || r.UpstreamModel == nil || *r.UpstreamModel != "claude-sonnet-4-5-v2" ||
		r.UsageIn == nil || *r.UsageIn != 100 || r.UsageOut == nil || *r.UsageOut != 55 {
		t.Fatalf("record = %+v", r)
	}
}

func TestEndpoint_CountTokensPassthrough(t *testing.T) {
	gotPath := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"input_tokens":42}`)
	}))
	defer upstream.Close()

	_, handler := newTestStack(t, upstream.URL)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp := postJSON(t, srv.Client(), srv.URL+"/v1/messages/count_tokens", `{"model":"claude-sonnet-4-5","messages":[]}`,
		map[string]string{"Authorization": "Bearer " + testClientKey})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "42") {
		t.Fatalf("count_tokens = %d %s", resp.StatusCode, body)
	}
	if p := <-gotPath; p != "/v1/messages/count_tokens" {
		t.Fatalf("upstream path = %q", p)
	}
}

func TestEndpoint_UpstreamUnreachable(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	_, handler := newTestStack(t, deadURL)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp := postJSON(t, srv.Client(), srv.URL+"/v1/messages", `{"model":"claude-sonnet-4-5"}`,
		map[string]string{"Authorization": "Bearer " + testClientKey})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(body, "api_error") {
		t.Fatalf("unreachable = %d %s", resp.StatusCode, body)
	}
}
