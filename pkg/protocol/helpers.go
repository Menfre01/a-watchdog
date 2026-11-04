package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// userAgent 是发往上游请求的固定 UA(部分上游按 UA 区分流量策略)。
const userAgent = "awatchdog"

// decodeObject 把 JSON 请求体解析为保留未知字段与数字精度的对象
// (使用 json.Number 防大整数精度损失,见 design.md D1)。
func decodeObject(body []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("decode body: %w", err)
	}
	return obj, nil
}

// modelOf 提取请求体中的 model 字段。
func modelOf(body []byte) (string, error) {
	obj, err := decodeObject(body)
	if err != nil {
		return "", err
	}
	model, _ := obj["model"].(string)
	if model == "" {
		return "", fmt.Errorf("missing model field")
	}
	return model, nil
}

// rewriteModel 仅替换 model 字段后重新序列化,其余字段原样保留。
func rewriteModel(body []byte, model string) ([]byte, error) {
	obj, err := decodeObject(body)
	if err != nil {
		return nil, err
	}
	obj["model"] = model
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	return out, nil
}

// upstreamURL 拼接上游 URL:base_url(去尾斜杠)+ 入向路径 + 原查询串。
func upstreamURL(in UpstreamInput) string {
	u := strings.TrimRight(in.Provider.BaseURL, "/") + in.InboundPath
	if in.InboundQuery != "" {
		u += "?" + in.InboundQuery
	}
	return u
}

// copyWhitelisted 按白名单把入向请求头复制到出站请求(凭据等敏感头一律不透传)。
func copyWhitelisted(dst, src http.Header, allow ...string) {
	for _, k := range allow {
		for _, v := range src.Values(k) {
			dst.Add(k, v)
		}
	}
}

// marshalOrFallback 序列化固定结构;编码失败(理论不可达)时退化为占位体。
func marshalOrFallback(v any, fallback string) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(fallback)
	}
	return b
}
