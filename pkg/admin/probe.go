package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"ds-context/pkg/config/db"
)

// probeResult 是连通性测试的结果。
type probeResult struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Status int    `json:"status,omitempty"`
}

// probeBodyLimit 是失败时回显的上游错误体上限(字节)。
const probeBodyLimit = 512

// defaultAnthropicVersionForProbe 是探测 anthropic 上游时使用的版本头。
const defaultAnthropicVersionForProbe = "2023-06-01"

// probeProvider 以 provider 配置的「测试模型」发起一条最小对话请求(1 token 输出),
// 一次验证地址、凭据与模型可用性——不依赖各家上游的模型列表端点(设计见 design.md D7)。
func (a *Admin) probeProvider(ctx context.Context, p db.Provider) probeResult {
	var path string
	switch p.Protocol {
	case "anthropic-messages":
		path = "/v1/messages"
	case "openai-chat":
		path = "/v1/chat/completions"
	default:
		return probeResult{Detail: "unknown protocol: " + p.Protocol}
	}
	if p.ProbeModel == "" {
		return probeResult{Detail: "未配置测试模型:请填写一个可用于验证的模型名后再测试连接"}
	}

	payload, err := json.Marshal(map[string]any{
		"model":      p.ProbeModel,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
	})
	if err != nil {
		return probeResult{Detail: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.BaseUrl, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return probeResult{Detail: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	switch p.Protocol {
	case "anthropic-messages":
		req.Header.Set("x-api-key", p.ApiKey)
		req.Header.Set("anthropic-version", defaultAnthropicVersionForProbe)
	case "openai-chat":
		req.Header.Set("Authorization", "Bearer "+p.ApiKey)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return probeResult{Detail: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return probeResult{
			OK:     true,
			Detail: fmt.Sprintf("verified: POST %s model=%s", path, p.ProbeModel),
			Status: resp.StatusCode,
		}
	}
	// 失败:回显少量上游错误体,给出可读原因。
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, probeBodyLimit))
	detail := fmt.Sprintf("upstream returned %d", resp.StatusCode)
	if trimmed := strings.TrimSpace(string(snippet)); trimmed != "" {
		detail += ": " + trimmed
	}
	return probeResult{Detail: detail, Status: resp.StatusCode}
}
