// Package protocol 实现对话协议的入向解析、出向构造、流式转发与错误形状。
// C1 提供两条同协议直通链路(openai-chat 与 anthropic-messages);
// 适配器接口为 C3 的跨协议 IR 转换预留(设计见 design.md D1/D2)。
package protocol

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrUnknownProtocol 表示引用了未注册的协议适配器。
var ErrUnknownProtocol = errors.New("protocol: unknown protocol")

// ProviderTarget 是路由选定的上游接入点。
type ProviderTarget struct {
	Protocol string // 上游协议标识(取 provider.protocol)
	BaseURL  string
	APIKey   string
	Model    string // 上游模型名(替换入向 model)
}

// UpstreamInput 是构造上游请求所需的输入。
type UpstreamInput struct {
	InboundPath  string      // 入向路径(如 /v1/messages/count_tokens)
	InboundQuery string      // 原始查询串(如 beta=true)
	Header       http.Header // 入向请求头(适配器按白名单决定转发)
	Body         []byte      // 入向请求体
	Provider     ProviderTarget
}

// Adapter 是单个对话协议的适配器:入向解析与出向构造成对实现。
type Adapter interface {
	// Protocol 返回协议标识(与 provider.protocol 取值一致)。
	Protocol() string
	// ParseModel 从入向请求体提取 model 字段(路由解析用)。
	ParseModel(body []byte) (string, error)
	// BuildUpstream 构造发往上游的 HTTP 请求(含模型改写与凭据替换)。
	BuildUpstream(ctx context.Context, in UpstreamInput) (*http.Request, error)
	// ErrorResponse 构造该协议形状的网关错误响应体(鉴权 / 路由未命中等)。
	ErrorResponse(status int, kind, message string) (contentType string, body []byte)
}

// registry 是协议适配器的编译期注册表:运行时无法引入新实现。
var registry = map[string]Adapter{}

// Register 注册协议适配器(空名或重名返回错误)。
func Register(a Adapter) error {
	name := a.Protocol()
	if name == "" {
		return errors.New("protocol: empty protocol name")
	}
	if _, dup := registry[name]; dup {
		return fmt.Errorf("protocol: duplicate adapter: %q", name)
	}
	registry[name] = a
	return nil
}

// Get 返回指定协议的适配器;未注册时返回 ErrUnknownProtocol。
func Get(protocol string) (Adapter, error) {
	a, ok := registry[protocol]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProtocol, protocol)
	}
	return a, nil
}

// init 在编译期登记 C1 的两条直通链路;注册失败属编程错误,直接终止。
func init() {
	if err := Register(openaiChatAdapter{}); err != nil {
		panic(err)
	}
	if err := Register(anthropicAdapter{}); err != nil {
		panic(err)
	}
}
