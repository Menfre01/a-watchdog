package protocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ds-context/pkg/chain"
)

func TestBuildUpstream_InvalidBaseURL(t *testing.T) {
	in := UpstreamInput{
		InboundPath: "/v1/messages",
		Body:        []byte(`{"model":"m"}`),
		Provider:    ProviderTarget{BaseURL: "http://[::1", APIKey: "k", Model: "mm"},
	}
	if _, err := (openaiChatAdapter{}).BuildUpstream(context.Background(), in); err == nil {
		t.Fatalf("openai BuildUpstream(invalid url) error = nil")
	}
	if _, err := (anthropicAdapter{}).BuildUpstream(context.Background(), in); err == nil {
		t.Fatalf("anthropic BuildUpstream(invalid url) error = nil")
	}
}

func TestProxy_HandleUnknownProviderProtocol(t *testing.T) {
	ctx := &chain.Context{
		W:         httptest.NewRecorder(),
		R:         httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil),
		StartedAt: time.Now(),
		Route:     &chain.RouteDecision{ProviderProtocol: "no-such-protocol"},
	}
	err := NewProxy().Handle(ctx)
	if err == nil || !strings.Contains(err.Error(), "unknown protocol") {
		t.Fatalf("Handle() error = %v, want unknown protocol", err)
	}
}

func TestSSEParser_OversizeBufferDiscarded(t *testing.T) {
	p := &sseParser{extract: openaiSSEUsage}
	// 无换行的超长输入必须被丢弃,避免无限累积。
	p.Write(make([]byte, maxSSELine+1))
	if len(p.buf) != 0 {
		t.Fatalf("oversize buffer kept: %d bytes", len(p.buf))
	}
	// 丢弃后仍可继续处理正常事件。
	p.Write([]byte(`data: {"usage":{"prompt_tokens":3,"completion_tokens":4}}` + "\n\n"))
	if u := p.Usage(); u.In == nil || *u.In != 3 {
		t.Fatalf("usage after discard = %+v", u)
	}
}
