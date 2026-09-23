package protocol

import (
	"bytes"
	"context"
	"net/http"
)

// anthropicAdapter 实现 Anthropic Messages 协议的直通适配。
type anthropicAdapter struct{}

// 编译期接口一致性断言(对应任务 4.1 的验证要求)。
var _ Adapter = anthropicAdapter{}

// defaultAnthropicVersion 是入向缺失 anthropic-version 时的兜底值(上游必需该头)。
const defaultAnthropicVersion = "2023-06-01"

// Protocol 实现 Adapter。
func (anthropicAdapter) Protocol() string { return "anthropic-messages" }

// ParseModel 实现 Adapter。
func (anthropicAdapter) ParseModel(body []byte) (string, error) {
	return modelOf(body)
}

// BuildUpstream 实现 Adapter:开放列表转发 anthropic-* 头,凭据替换为 x-api-key。
func (anthropicAdapter) BuildUpstream(ctx context.Context, in UpstreamInput) (*http.Request, error) {
	body, err := rewriteModel(in.Body, in.Provider.Model)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL(in), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	// 开放列表:与协议能力相关的头原样转发;凭据头绝不透传。
	copyWhitelisted(req.Header, in.Header, "Content-Type", "Accept", "Anthropic-Version", "Anthropic-Beta")
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("Anthropic-Version") == "" {
		req.Header.Set("Anthropic-Version", defaultAnthropicVersion)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("x-api-key", in.Provider.APIKey)
	return req, nil
}

// ErrorResponse 实现 Adapter:Anthropic 形状错误响应。
func (anthropicAdapter) ErrorResponse(status int, _ string, message string) (string, []byte) {
	inner := "invalid_request_error"
	switch {
	case status == http.StatusUnauthorized:
		inner = "authentication_error"
	case status == http.StatusForbidden:
		inner = "permission_error"
	case status == http.StatusNotFound:
		inner = "not_found_error"
	case status == http.StatusTooManyRequests:
		inner = "rate_limit_error"
	case status >= http.StatusInternalServerError:
		inner = "api_error"
	}
	payload := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    inner,
			"message": message,
		},
	}
	body := marshalOrFallback(payload, `{"type":"error","error":{"type":"api_error","message":"internal error"}}`)
	return "application/json", body
}
