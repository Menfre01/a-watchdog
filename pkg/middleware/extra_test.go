package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"ds-context/pkg/chain"
)

func TestMiddlewareIdentity(t *testing.T) {
	store := newStore(t)
	auth := NewAuth(store.Snapshot)
	route := NewRoute(store.Snapshot)
	rec := NewRecorder(context.Background(), store, discardLogger())

	if auth.Name() != "auth" || auth.Stage() != chain.StageAdmission {
		t.Fatalf("auth identity = %q/%q", auth.Name(), auth.Stage())
	}
	if route.Name() != "route" || route.Stage() != chain.StageRouting {
		t.Fatalf("route identity = %q/%q", route.Name(), route.Stage())
	}
	if rec.Name() != "record" || rec.Stage() != chain.StagePostprocess {
		t.Fatalf("record identity = %q/%q", rec.Name(), rec.Stage())
	}
}

func TestRespond_UnknownProtocolFallback(t *testing.T) {
	rej := respond("no-such-protocol", http.StatusTeapot, "x", "something failed")
	if rej.StatusCode != http.StatusTeapot || rej.ContentType != "application/json" {
		t.Fatalf("reject = %+v", rej)
	}
	if !strings.Contains(string(rej.Body), "something failed") {
		t.Fatalf("body = %s", rej.Body)
	}
}

func TestRoute_UnknownProtocolIsInternalError(t *testing.T) {
	store := newStore(t)
	mw := NewRoute(store.Snapshot)
	err := mw.Handle(newCtx("no-such-protocol", nil, `{}`), okHandler(new(bool)))
	if err == nil {
		t.Fatalf("Handle() error = nil, want internal error")
	}
	var rej *chain.RejectError
	if errors.As(err, &rej) {
		t.Fatalf("装配错误不应是受控拒绝: %v", err)
	}
}

func TestSnapshotRecord_Branches(t *testing.T) {
	// 内部错误 → 502 + kind=internal;无 route 且请求体不可解析 → 模型为空。
	ctx := &chain.Context{Protocol: "openai-chat", StartedAt: time.Now(), Body: []byte(`not-json`)}
	p := snapshotRecord(ctx, errors.New("boom"))
	if p.params.StatusCode != http.StatusBadGateway || p.params.ErrorKind == nil || *p.params.ErrorKind != "internal" {
		t.Fatalf("internal record = %+v", p)
	}
	if p.params.RequestedModel != "" {
		t.Fatalf("RequestedModel = %q, want empty", p.params.RequestedModel)
	}

	// 受控拒绝 → 状态与 kind 取自 RejectError。
	rej := &chain.RejectError{StatusCode: http.StatusTooManyRequests, ContentType: "application/json", Body: []byte(`{}`), Kind: "rate_limit"}
	p = snapshotRecord(ctx, rej)
	if p.params.StatusCode != http.StatusTooManyRequests || p.params.ErrorKind == nil || *p.params.ErrorKind != "rate_limit" {
		t.Fatalf("reject record = %+v", p)
	}

	// 无 route 但请求体可解析 → 记录请求模型(尽力而为)。
	ctx2 := &chain.Context{Protocol: "openai-chat", StartedAt: time.Now(), Body: []byte(`{"model":"m1"}`)}
	p = snapshotRecord(ctx2, &chain.RejectError{StatusCode: 404, Body: []byte(`{}`), Kind: "model_not_found"})
	if p.params.RequestedModel != "m1" {
		t.Fatalf("RequestedModel = %q, want m1", p.params.RequestedModel)
	}
}

func TestSnapshotRecord_StreamInterrupted(t *testing.T) {
	// 响应已开始后流中断:保留上游真实状态码,错误仅归因。
	up := &chain.UpstreamOutcome{StatusCode: http.StatusOK, Stream: true}
	ctx := &chain.Context{
		Protocol:        "anthropic-messages",
		StartedAt:       time.Now(),
		ResponseStarted: true,
		Upstream:        up,
	}
	p := snapshotRecord(ctx, errors.New("read upstream: unexpected EOF"))
	if p.params.StatusCode != http.StatusOK || p.params.ErrorKind == nil || *p.params.ErrorKind != "stream_interrupted" {
		t.Fatalf("stream interrupted record = %+v, want 200/stream_interrupted", p)
	}
}
