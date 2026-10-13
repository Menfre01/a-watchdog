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

	// cacheRead / cacheCreation 是 Cache 的内部累积分量:Anthropic 分开上报缓存读
	// 与缓存写,两个分量可能落在不同事件里(message_start 与 message_delta)。
	// 分量必须独立更新,否则后到的事件会把先到的分量清零。
	cacheRead     *int64
	cacheCreation *int64
}

// mergeCache 更新缓存分量并重算 Cache:分量为 nil 时保持原值。
func (u *Usage) mergeCache(read, creation *int64) {
	if read != nil {
		u.cacheRead = read
	}
	if creation != nil {
		u.cacheCreation = creation
	}
	if u.cacheRead == nil && u.cacheCreation == nil {
		return
	}
	var total int64
	if u.cacheRead != nil {
		total += *u.cacheRead
	}
	if u.cacheCreation != nil {
		total += *u.cacheCreation
	}
	u.Cache = &total
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

// cacheCreationDetail 是新版 API 的缓存写入明细:按 TTL 拆分缓存写入量。
type cacheCreationDetail struct {
	Ephemeral5mInputTokens *int64 `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens *int64 `json:"ephemeral_1h_input_tokens"`
}

// total 返回明细求和;两个字段都缺失时返回 nil(此时应退回汇总数字键)。
func (d *cacheCreationDetail) total() *int64 {
	if d == nil || (d.Ephemeral5mInputTokens == nil && d.Ephemeral1hInputTokens == nil) {
		return nil
	}
	var sum int64
	if d.Ephemeral5mInputTokens != nil {
		sum += *d.Ephemeral5mInputTokens
	}
	if d.Ephemeral1hInputTokens != nil {
		sum += *d.Ephemeral1hInputTokens
	}
	return &sum
}

// UnmarshalJSON 实现 json.Unmarshaler:非对象值直接忽略。
// 部分实现会把缓存写入量误发在 cache_creation 键下(数字而非明细对象);类型不符
// 若向上冒泡,整个事件的解析会失败,连带丢掉同一事件里的 output_tokens 等字段。
func (d *cacheCreationDetail) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || data[0] != '{' {
		return nil
	}
	type detail cacheCreationDetail
	var parsed detail
	if err := json.Unmarshal(data, &parsed); err != nil {
		// 结构不符同样忽略:该字段缺失只影响缓存写入量,不牵连其他用量字段。
		return nil
	}
	*d = cacheCreationDetail(parsed)
	return nil
}

// anthropicUsage 是 Anthropic 事件里的 usage 负载(message_start 与 message_delta 共用)。
type anthropicUsage struct {
	InputTokens              *int64               `json:"input_tokens"`
	OutputTokens             *int64               `json:"output_tokens"`
	CacheReadInputTokens     *int64               `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64               `json:"cache_creation_input_tokens"`
	CacheCreation            *cacheCreationDetail `json:"cache_creation"`
}

// cacheCreation 返回缓存写入量:明细对象优先,缺失时退回汇总数字键。
func (a *anthropicUsage) cacheCreation() *int64 {
	if total := a.CacheCreation.total(); total != nil {
		return total
	}
	return a.CacheCreationInputTokens
}

// anthropicSSEUsage 从 Anthropic SSE 事件提取用量(message_start 的 message.usage
// 与 message_delta 的 usage)。
func anthropicSSEUsage(data []byte, u *Usage) {
	var ev struct {
		Message *struct {
			Usage *anthropicUsage `json:"usage"`
		} `json:"message"`
		Usage *anthropicUsage `json:"usage"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}
	if ev.Message != nil && ev.Message.Usage != nil {
		m := ev.Message.Usage
		if m.InputTokens != nil {
			u.In = m.InputTokens
		}
		// message_start 的 output_tokens 是占位值(固定为 1),不是最终输出量;
		// 真实值只在 message_delta 出现,取占位值会把中断的流记成输出 1 token。
		u.mergeCache(m.CacheReadInputTokens, m.cacheCreation())
	}
	if ev.Usage != nil {
		d := ev.Usage
		if d.OutputTokens != nil {
			u.Out = d.OutputTokens
		}
		// 新版本 API 会在 message_delta 的 usage 中携带累计 cache 用量。
		u.mergeCache(d.CacheReadInputTokens, d.cacheCreation())
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
		u.mergeCache(d.CachedTokens, nil)
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

// usageFields 是响应体 usage 对象的字段并集:同时兼容 OpenAI 的 prompt/completion
// 键名与 Anthropic 的 input/output 键名。
type usageFields struct {
	PromptTokens             *int64               `json:"prompt_tokens"`
	CompletionTokens         *int64               `json:"completion_tokens"`
	InputTokens              *int64               `json:"input_tokens"`
	OutputTokens             *int64               `json:"output_tokens"`
	CacheReadInputTokens     *int64               `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64               `json:"cache_creation_input_tokens"`
	CacheCreation            *cacheCreationDetail `json:"cache_creation"`
	PromptTokensDetails      *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// parseJSONUsage 解析非流式响应体中的 usage(兼容 OpenAI 与 Anthropic 两种键名)。
func parseJSONUsage(body []byte, u *Usage) {
	var doc struct {
		Usage usageFields `json:"usage"`
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
	// 缓存写入量可能以明细对象(新版)或汇总数字键(旧版)给出。
	read := use.CacheReadInputTokens
	creation := use.CacheCreation.total()
	if creation == nil {
		creation = use.CacheCreationInputTokens
	}
	// Anthropic 的读写分量优先;两者都缺失时退回 OpenAI 的缓存读命中数。
	if read == nil && creation == nil && use.PromptTokensDetails != nil {
		read = use.PromptTokensDetails.CachedTokens
	}
	u.mergeCache(read, creation)
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
