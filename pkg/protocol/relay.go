package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Usage 是一次上游响应的用量口径(解析不到时字段为空)。
type Usage struct {
	In    *int64
	Out   *int64
	Cache *int64
}

// usageParser 是增量解析响应字节的用量解析器(解析失败静默忽略)。
type usageParser interface {
	Write(b []byte)
	Usage() Usage
}

// Observer 在转发的同时旁路解析响应字节,尽力提取用量;
// 它只读、绝不阻塞,解析失败不影响转发(对应 design.md D1 的风险隔离)。
type Observer struct {
	parser usageParser
}

// NewObserver 依据协议与响应内容类型创建观察器;无法识别时返回空操作观察器。
func NewObserver(protocolName, contentType string) *Observer {
	switch {
	case strings.Contains(contentType, "text/event-stream"):
		switch protocolName {
		case "anthropic-messages":
			return &Observer{parser: &sseParser{extract: anthropicSSEUsage}}
		case "openai-chat":
			return &Observer{parser: &sseParser{extract: openaiSSEUsage}}
		}
	case strings.Contains(contentType, "application/json"):
		return &Observer{parser: &jsonUsageParser{}}
	}
	return &Observer{}
}

// Write 喂入响应字节。
func (o *Observer) Write(p []byte) {
	if o.parser != nil {
		o.parser.Write(p)
	}
}

// Usage 返回已解析出的用量(在转发结束后调用)。
func (o *Observer) Usage() Usage {
	if o.parser == nil {
		return Usage{}
	}
	return o.parser.Usage()
}

// maxSSELine 是单行 SSE 数据的防御上限:异常超长输入直接丢弃,避免无限累积。
const maxSSELine = 1 << 20

// sseParser 逐行收集 SSE data 负载并交给提取器解析。
type sseParser struct {
	extract func(data []byte, u *Usage)
	buf     []byte
	usage   Usage
}

// Write 实现 usageParser。
func (p *sseParser) Write(b []byte) {
	p.buf = append(p.buf, b...)
	for {
		i := bytes.IndexByte(p.buf, '\n')
		if i < 0 {
			break
		}
		line := bytes.TrimRight(p.buf[:i], "\r")
		p.buf = p.buf[i+1:]
		if rest, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			p.extract(bytes.TrimSpace(rest), &p.usage)
		}
	}
	if len(p.buf) > maxSSELine {
		p.buf = nil
	}
}

// Usage 实现 usageParser。
func (p *sseParser) Usage() Usage { return p.usage }

// anthropicSSEUsage 从 Anthropic SSE 事件提取用量(message_start 的 message.usage
// 与 message_delta 的 usage)。
func anthropicSSEUsage(data []byte, u *Usage) {
	var ev struct {
		Message *struct {
			Usage *struct {
				InputTokens              *int64 `json:"input_tokens"`
				OutputTokens             *int64 `json:"output_tokens"`
				CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
				CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Usage *struct {
			OutputTokens             *int64 `json:"output_tokens"`
			CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}
	if ev.Message != nil && ev.Message.Usage != nil {
		m := ev.Message.Usage
		if m.InputTokens != nil {
			u.In = m.InputTokens
		}
		if m.OutputTokens != nil {
			u.Out = m.OutputTokens
		}
		if r, c := m.CacheReadInputTokens, m.CacheCreationInputTokens; r != nil || c != nil {
			var total int64
			if r != nil {
				total += *r
			}
			if c != nil {
				total += *c
			}
			u.Cache = &total
		}
	}
	if ev.Usage != nil {
		if ev.Usage.OutputTokens != nil {
			u.Out = ev.Usage.OutputTokens
		}
		// 新版本 API 会在 message_delta 的 usage 中携带累计 cache 用量。
		if r, c := ev.Usage.CacheReadInputTokens, ev.Usage.CacheCreationInputTokens; r != nil || c != nil {
			var total int64
			if r != nil {
				total += *r
			}
			if c != nil {
				total += *c
			}
			u.Cache = &total
		}
	}
}

// openaiSSEUsage 从 OpenAI 流式块提取用量(仅当客户端请求 include_usage 时存在)。
func openaiSSEUsage(data []byte, u *Usage) {
	var chunk struct {
		Usage *struct {
			PromptTokens        *int64 `json:"prompt_tokens"`
			CompletionTokens    *int64 `json:"completion_tokens"`
			PromptTokensDetails *struct {
				CachedTokens *int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil {
		return
	}
	if chunk.Usage == nil {
		return
	}
	if chunk.Usage.PromptTokens != nil {
		u.In = chunk.Usage.PromptTokens
	}
	if chunk.Usage.CompletionTokens != nil {
		u.Out = chunk.Usage.CompletionTokens
	}
	if d := chunk.Usage.PromptTokensDetails; d != nil && d.CachedTokens != nil {
		u.Cache = d.CachedTokens
	}
}

// maxJSONBodyUsage 是非流式响应解析用量的体量上限,超过则放弃提取。
const maxJSONBodyUsage = 1 << 20

// jsonUsageParser 面向非流式 JSON 响应:累积(有上限)后在结束时一次性解析。
type jsonUsageParser struct {
	buf     []byte
	stopped bool
	usage   Usage
}

// Write 实现 usageParser。
func (p *jsonUsageParser) Write(b []byte) {
	if p.stopped {
		return
	}
	if len(p.buf)+len(b) > maxJSONBodyUsage {
		p.stopped = true
		p.buf = nil
		return
	}
	p.buf = append(p.buf, b...)
}

// Usage 实现 usageParser:惰性解析一次。
func (p *jsonUsageParser) Usage() Usage {
	if p.buf != nil {
		parseJSONUsage(p.buf, &p.usage)
		p.buf = nil
	}
	return p.usage
}

// parseJSONUsage 解析非流式响应体中的 usage(兼容 OpenAI 与 Anthropic 两种键名)。
func parseJSONUsage(body []byte, u *Usage) {
	var doc struct {
		Usage struct {
			PromptTokens             *int64 `json:"prompt_tokens"`
			CompletionTokens         *int64 `json:"completion_tokens"`
			InputTokens              *int64 `json:"input_tokens"`
			OutputTokens             *int64 `json:"output_tokens"`
			CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
			PromptTokensDetails      *struct {
				CachedTokens *int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return
	}
	use := doc.Usage
	switch {
	case use.PromptTokens != nil:
		u.In = use.PromptTokens
	case use.InputTokens != nil:
		u.In = use.InputTokens
	}
	switch {
	case use.CompletionTokens != nil:
		u.Out = use.CompletionTokens
	case use.OutputTokens != nil:
		u.Out = use.OutputTokens
	}
	switch {
	case use.CacheReadInputTokens != nil || use.CacheCreationInputTokens != nil:
		var total int64
		if use.CacheReadInputTokens != nil {
			total += *use.CacheReadInputTokens
		}
		if use.CacheCreationInputTokens != nil {
			total += *use.CacheCreationInputTokens
		}
		u.Cache = &total
	case use.PromptTokensDetails != nil && use.PromptTokensDetails.CachedTokens != nil:
		u.Cache = use.PromptTokensDetails.CachedTokens
	}
}

// relayBufferSize 是流式转发的分块大小。
const relayBufferSize = 32 * 1024

// RelayStream 把上游响应体逐块转发到 w(每块立即 Flush),同时旁路喂给观察器。
// 不做任何整体缓冲;客户端断开时由上游请求的 context 取消向上游传播。
func RelayStream(w http.ResponseWriter, rc io.Reader, obs *Observer) error {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, relayBufferSize)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return fmt.Errorf("protocol: write to client: %w", werr)
			}
			obs.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("protocol: read upstream: %w", err)
		}
	}
}
