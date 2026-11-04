package protocol

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"awatchdog/pkg/chain"
)

// 上游连接与首字节超时的默认值;流式响应整体耗时不设上限。
const (
	defaultConnectTimeout = 10 * time.Second
	defaultHeaderTimeout  = 60 * time.Second
)

// Proxy 是责任链的终结点:执行上游调用、把响应原样流式回传,并填充调用元数据。
type Proxy struct {
	client *http.Client
}

// NewProxy 创建 Proxy:仅设连接 / 首字节超时,不设整体超时(支持长流式)。
func NewProxy() *Proxy {
	return &Proxy{client: newUpstreamClient(defaultConnectTimeout, defaultHeaderTimeout)}
}

// NewProxyWithClient 用指定 HTTP 客户端创建 Proxy(测试注入用)。
func NewProxyWithClient(c *http.Client) *Proxy {
	return &Proxy{client: c}
}

// newUpstreamClient 构造上游 HTTP 客户端:连接 / 首字节超时,无整体超时。
func newUpstreamClient(connectTimeout, headerTimeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: connectTimeout}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   connectTimeout,
		ExpectContinueTimeout: time.Second,
		ResponseHeaderTimeout: headerTimeout,
	}
	// 不设置 Client.Timeout:流式响应整体耗时可远超首字节超时。
	return &http.Client{
		Transport: transport,
		// 禁止跟随重定向:Go 跨域重定向只剥离 Authorization/Cookie,
		// x-api-key 会随重定向转发到新主机,导致上游凭据外泄(3xx 原样透传给客户端)。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Handle 实现链的终结点语义:构造上游请求 → 调用 → 原样回写状态与头 →
// 流式转发响应体(旁路提取用量)。上游错误响应因此原样透传、不加信封。
func (p *Proxy) Handle(ctx *chain.Context) error {
	if ctx.Route == nil {
		return fmt.Errorf("protocol: missing route decision")
	}
	adapter, err := Get(ctx.Route.ProviderProtocol)
	if err != nil {
		return err
	}
	upReq, err := adapter.BuildUpstream(ctx.R.Context(), UpstreamInput{
		InboundPath:  ctx.R.URL.Path,
		InboundQuery: ctx.R.URL.RawQuery,
		Header:       ctx.R.Header,
		Body:         ctx.Body,
		Provider: ProviderTarget{
			Protocol: ctx.Route.ProviderProtocol,
			BaseURL:  ctx.Route.BaseURL,
			APIKey:   ctx.Route.APIKey,
			Model:    ctx.Route.UpstreamModel,
		},
	})
	if err != nil {
		return fmt.Errorf("protocol: build upstream request: %w", err)
	}

	resp, err := p.client.Do(upReq)
	if err != nil {
		return fmt.Errorf("protocol: upstream call: %w", err)
	}
	// 响应体在本函数内消费,结束时释放连接。
	defer func() { _ = resp.Body.Close() }()

	// 状态码与响应头原样回写(上游错误体也因此原样到客户端)。
	for k, vv := range resp.Header {
		if strings.EqualFold(k, "Trailer") {
			continue // 逐跳头:经客户端栈转发会声明永远不会出现的 trailer
		}
		for _, v := range vv {
			ctx.W.Header().Add(k, v)
		}
	}
	ctx.W.WriteHeader(resp.StatusCode)
	ctx.ResponseStarted = true

	contentType := resp.Header.Get("Content-Type")
	observer := NewObserver(ctx.Route.ProviderProtocol, contentType)
	outcome := &chain.UpstreamOutcome{
		StatusCode: resp.StatusCode,
		Stream:     strings.Contains(contentType, "text/event-stream"),
	}
	ctx.Upstream = outcome

	relayErr := RelayStream(ctx.W, resp.Body, observer)
	ctx.ResponseBody, ctx.ResponseTruncated = observer.Captured()
	usage := observer.Usage()
	outcome.UsageIn, outcome.UsageOut, outcome.UsageCache = usage.In, usage.Out, usage.Cache
	return relayErr
}
