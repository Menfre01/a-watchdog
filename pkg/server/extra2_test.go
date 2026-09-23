package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds-context/pkg/chain"
)

func TestEndpoint_RejectAfterResponseStarted(t *testing.T) {
	// 响应已开始后链路返回受控拒绝:只能记日志,不再写出错误响应。
	s := New(func(ctx *chain.Context) error {
		ctx.ResponseStarted = true
		return &chain.RejectError{StatusCode: 401, ContentType: "application/json", Body: []byte(`{}`), Kind: "auth"}
	}, discardLogger())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"m"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("reject-after-start = %d %q, want 未写出", rec.Code, rec.Body.String())
	}
}

func TestEndpoint_ClientDisconnectNoWrite(t *testing.T) {
	// 客户端已断开(请求 ctx 已取消)时出错:只记 Info,不再写响应。
	s := New(func(*chain.Context) error { return errors.New("upstream boom") }, discardLogger())
	reqCtx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(reqCtx, http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"m"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("client-disconnect path = %d %q, want 未写出", rec.Code, rec.Body.String())
	}
}

func TestEndpoint_PanicIsContained(t *testing.T) {
	// 链上环节 panic 时:进程不受影响,响应未开始则补写协议形状 502。
	s := New(func(*chain.Context) error { panic("boom") }, discardLogger())
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"m"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "api_error") {
		t.Fatalf("panic containment = %d %q, want 502 协议形状", rec.Code, rec.Body.String())
	}
}
