package protocol

import (
	"bytes"
	"context"
	"net/http"
)

// openaiChatAdapter 实现 OpenAI Chat Completions 协议的直通适配。
type openaiChatAdapter struct{}

// 编译期接口一致性断言(对应任务 4.1 的验证要求)。
var _ Adapter = openaiChatAdapter{}

// Protocol 实现 Adapter。
func (openaiChatAdapter) Protocol() string { return "openai-chat" }

// ParseModel 实现 Adapter。
func (openaiChatAdapter) ParseModel(body []byte) (string, error) {
	return modelOf(body)
}

// BuildUpstream 实现 Adapter:改写 model、替换凭据(x-api-key/authorization 不透传)。
func (openaiChatAdapter) BuildUpstream(ctx context.Context, in UpstreamInput) (*http.Request, error) {
	body, err := rewriteModel(in.Body, in.Provider.Model)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL(in), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	copyWhitelisted(req.Header, in.Header, "Content-Type", "Accept")
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", "Bearer "+in.Provider.APIKey)
	return req, nil
}

// ErrorResponse 实现 Adapter:OpenAI 形状错误响应。
func (openaiChatAdapter) ErrorResponse(status int, kind, message string) (string, []byte) {
	typ := "invalid_request_error"
	if status >= http.StatusInternalServerError {
		typ = "server_error"
	}
	payload := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    typ,
			"code":    kind,
		},
	}
	body := marshalOrFallback(payload, `{"error":{"message":"internal error","type":"server_error"}}`)
	return "application/json", body
}
