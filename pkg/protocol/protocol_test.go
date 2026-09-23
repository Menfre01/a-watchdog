package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

// fakeAdapter 是注册表测试用的最小适配器。
type fakeAdapter struct{ name string }

// Protocol 实现 Adapter。
func (f fakeAdapter) Protocol() string { return f.name }

// ParseModel 实现 Adapter。
func (f fakeAdapter) ParseModel([]byte) (string, error) { return "", errors.New("not implemented") }

// BuildUpstream 实现 Adapter。
func (f fakeAdapter) BuildUpstream(context.Context, UpstreamInput) (*http.Request, error) {
	return nil, errors.New("not implemented")
}

// ErrorResponse 实现 Adapter。
func (f fakeAdapter) ErrorResponse(int, string, string) (string, []byte) { return "", nil }

func TestRegistry_C1AdaptersRegistered(t *testing.T) {
	for _, name := range []string{"openai-chat", "anthropic-messages"} {
		a, err := Get(name)
		if err != nil || a.Protocol() != name {
			t.Fatalf("Get(%q) = %v, %v", name, a, err)
		}
	}
	if _, err := Get("nope"); !errors.Is(err, ErrUnknownProtocol) {
		t.Fatalf("Get(unknown) error = %v, want ErrUnknownProtocol", err)
	}
	if err := Register(fakeAdapter{name: "openai-chat"}); err == nil {
		t.Fatalf("duplicate Register() accepted")
	}
	if err := Register(fakeAdapter{name: ""}); err == nil {
		t.Fatalf("empty-name Register() accepted")
	}
}

func TestAdapters_ParseModel(t *testing.T) {
	for _, a := range []Adapter{openaiChatAdapter{}, anthropicAdapter{}} {
		t.Run(a.Protocol(), func(t *testing.T) {
			model, err := a.ParseModel([]byte(`{"model":"claude-sonnet-4-5","messages":[]}`))
			if err != nil || model != "claude-sonnet-4-5" {
				t.Fatalf("ParseModel() = %q, %v", model, err)
			}
			if _, err := a.ParseModel([]byte(`{"messages":[]}`)); err == nil {
				t.Fatalf("ParseModel(missing model) accepted")
			}
			if _, err := a.ParseModel([]byte(`not-json`)); err == nil {
				t.Fatalf("ParseModel(invalid json) accepted")
			}
		})
	}
}

func TestOpenAI_BuildUpstream(t *testing.T) {
	in := UpstreamInput{
		InboundPath:  "/v1/chat/completions",
		InboundQuery: "trace=1",
		Header: http.Header{
			"Content-Type":  {"application/json"},
			"Authorization": {"Bearer client-key-should-not-leak"},
			"X-Custom":      {"secret"},
			"Accept":        {"text/event-stream"},
		},
		Body: []byte(`{"model":"alias","stream":true,"messages":[{"role":"user","content":"hi"}],"max_tokens":128}`),
		Provider: ProviderTarget{
			Protocol: "openai-chat", BaseURL: "https://api.example.com/",
			APIKey: "sk-upstream", Model: "up-model",
		},
	}
	req, err := (openaiChatAdapter{}).BuildUpstream(context.Background(), in)
	if err != nil {
		t.Fatalf("BuildUpstream() error = %v", err)
	}
	if got := req.URL.String(); got != "https://api.example.com/v1/chat/completions?trace=1" {
		t.Fatalf("URL = %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-upstream" {
		t.Fatalf("Authorization = %q, want provider credential", got)
	}
	if req.Header.Get("X-Custom") != "" {
		t.Fatalf("X-Custom 不应被转发(白名单之外)")
	}
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q, want passthrough", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("upstream body invalid: %v", err)
	}
	if sent["model"] != "up-model" {
		t.Fatalf("model = %v, want up-model", sent["model"])
	}
	if sent["stream"] != true || sent["max_tokens"] != float64(128) {
		t.Fatalf("其余字段应原样保留: %v", sent)
	}
	msgs, _ := sent["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", sent["messages"])
	}
}

func TestAnthropic_BuildUpstream(t *testing.T) {
	in := UpstreamInput{
		InboundPath:  "/v1/messages",
		InboundQuery: "beta=true",
		Header: http.Header{
			"X-Api-Key":         {"client-key-should-not-leak"},
			"Anthropic-Version": {"2023-06-01"},
			"Anthropic-Beta":    {"oauth-2025-04-20"},
		},
		Body: []byte(`{"model":"alias","stream":true}`),
		Provider: ProviderTarget{
			Protocol: "anthropic-messages", BaseURL: "https://api.anthropic.com",
			APIKey: "sk-ant-up", Model: "up-model",
		},
	}
	req, err := (anthropicAdapter{}).BuildUpstream(context.Background(), in)
	if err != nil {
		t.Fatalf("BuildUpstream() error = %v", err)
	}
	if got := req.URL.String(); got != "https://api.anthropic.com/v1/messages?beta=true" {
		t.Fatalf("URL = %q", got)
	}
	if got := req.Header.Get("X-Api-Key"); got != "sk-ant-up" {
		t.Fatalf("X-Api-Key = %q, want provider credential", got)
	}
	if got := req.Header.Get("Anthropic-Version"); got != "2023-06-01" {
		t.Fatalf("Anthropic-Version = %q, want passthrough", got)
	}
	if got := req.Header.Get("Anthropic-Beta"); got != "oauth-2025-04-20" {
		t.Fatalf("Anthropic-Beta = %q, want passthrough", got)
	}

	// 入向缺失 anthropic-version 时使用默认值。
	in.Header = http.Header{}
	req2, err := (anthropicAdapter{}).BuildUpstream(context.Background(), in)
	if err != nil {
		t.Fatalf("BuildUpstream(default version) error = %v", err)
	}
	if got := req2.Header.Get("Anthropic-Version"); got != defaultAnthropicVersion {
		t.Fatalf("default Anthropic-Version = %q, want %q", got, defaultAnthropicVersion)
	}
}

func TestErrorResponse_Shapes(t *testing.T) {
	// OpenAI:404 未路由。
	ct, body := (openaiChatAdapter{}).ErrorResponse(http.StatusNotFound, "model_not_found", "model not routed")
	if ct != "application/json" {
		t.Fatalf("content type = %q", ct)
	}
	var oa struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &oa); err != nil {
		t.Fatalf("openai body invalid: %v", err)
	}
	if oa.Error.Code != "model_not_found" || oa.Error.Type != "invalid_request_error" || oa.Error.Message == "" {
		t.Fatalf("openai error = %+v", oa.Error)
	}

	// OpenAI:500 → server_error。
	_, body = (openaiChatAdapter{}).ErrorResponse(http.StatusInternalServerError, "internal", "boom")
	if err := json.Unmarshal(body, &oa); err != nil {
		t.Fatalf("openai 500 body invalid: %v", err)
	}
	if oa.Error.Type != "server_error" {
		t.Fatalf("openai 500 type = %q", oa.Error.Type)
	}

	// Anthropic:401 → authentication_error;404 → not_found_error。
	var an struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	ct, body = (anthropicAdapter{}).ErrorResponse(http.StatusUnauthorized, "auth", "invalid key")
	if ct != "application/json" {
		t.Fatalf("content type = %q", ct)
	}
	if err := json.Unmarshal(body, &an); err != nil {
		t.Fatalf("anthropic body invalid: %v", err)
	}
	if an.Type != "error" || an.Error.Type != "authentication_error" || an.Error.Message == "" {
		t.Fatalf("anthropic 401 = %+v", an)
	}
	_, body = (anthropicAdapter{}).ErrorResponse(http.StatusNotFound, "route_not_found", "no route")
	if err := json.Unmarshal(body, &an); err != nil {
		t.Fatalf("anthropic 404 body invalid: %v", err)
	}
	if an.Error.Type != "not_found_error" {
		t.Fatalf("anthropic 404 type = %q", an.Error.Type)
	}
}
