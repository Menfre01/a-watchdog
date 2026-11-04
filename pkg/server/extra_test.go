package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"awatchdog/pkg/chain"
)

func TestServer_WithConsoleAndBodyLimit(t *testing.T) {
	console := http.NewServeMux()
	console.HandleFunc("/console/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "console")
	})
	s := New(func(*chain.Context) error { return nil }, discardLogger()).WithConsole(console)
	s.maxBody = 8 // 测试用的小上限,覆盖限流分支
	handler := s.Handler()

	// 超过上限的请求体 → 400(按入向协议形状)。
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"this is longer than 8 bytes"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversize body = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_request_error") {
		t.Fatalf("oversize body shape = %s", rec.Body.String())
	}

	// 控制台已挂载。
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/console/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "console" {
		t.Fatalf("console mount = %d %q", rec.Code, rec.Body.String())
	}
}

func TestWriteRejected_UnknownProtocolFallback(t *testing.T) {
	s := New(func(*chain.Context) error { return nil }, discardLogger())
	rec := httptest.NewRecorder()
	s.writeRejected(rec, "no-such-protocol", http.StatusInternalServerError, "x", "boom")
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "internal error") {
		t.Fatalf("fallback = %d %q", rec.Code, rec.Body.String())
	}
}

func TestEndpoint_StreamInterruptedAfterStart(t *testing.T) {
	// 假上游宣称 1000 字节但只写一部分:转发中途出错,此时响应已开始,
	// 网关只能记日志、不再改写响应(覆盖 endpoint 的 ResponseStarted 分支)。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: partial\n\n")
	}))
	defer upstream.Close()

	store, handler := newTestStack(t, upstream.URL)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp := postJSON(t, srv.Client(), srv.URL+"/v1/messages", `{"model":"claude-sonnet-4-5","stream":true}`,
		map[string]string{"Authorization": "Bearer " + testClientKey})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	// 客户端读到部分数据即中断(忽略读取错误,验证状态已写出即可)。
	_, _ = io.ReadAll(resp.Body)

	eventually(t, func() bool {
		n, err := store.CountRequests(context.Background())
		return err == nil && n == 1
	})
}
