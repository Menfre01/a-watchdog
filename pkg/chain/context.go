package chain

import (
	"net/http"
	"time"
)

// ClientIdentity 是准入阶段确认的客户端身份(写入请求记录用)。
type ClientIdentity struct {
	KeyID   int64
	KeyName string
}

// RouteDecision 是路由/适配阶段产出的上游选择。
type RouteDecision struct {
	ClientModel      string // 请求 model 字段(模型目录可见名)
	ProviderID       int64
	ProviderName     string
	ProviderProtocol string // "openai-chat" / "anthropic-messages"
	BaseURL          string
	APIKey           string // 上游凭据:仅内存传递,严禁写入记录或日志
	UpstreamModel    string // 发往上游的模型名
}

// UpstreamOutcome 是上游调用(终结点)产生的元数据,供响应后处理阶段读取。
type UpstreamOutcome struct {
	StatusCode int
	Stream     bool
	UsageIn    *int64
	UsageOut   *int64
	UsageCache *int64
}

// Context 是单次请求的强类型上下文,承载跨环节数据。
// 环节之间只通过这些类型化字段传递业务数据,不使用弱类型键值容器
// (对应 request-pipeline 规范「强类型请求上下文」)。
type Context struct {
	// HTTP 层,由服务端在入口填充。
	W http.ResponseWriter
	R *http.Request

	// Protocol 是入向协议标识(如 "anthropic-messages" / "openai-chat")。
	Protocol string
	// StartedAt 是请求进入网关的时间(记录延迟用)。
	StartedAt time.Time
	// Body 是入向请求体的原始字节(服务端在入口读取,路由与转发共用)。
	Body []byte
	// ResponseStarted 标记响应已开始写出;此后出错只能记日志,不能再写错误响应。
	ResponseStarted bool

	// 阶段产物:按阶段写入、后续环节只读。
	ClientKey *ClientIdentity
	Route     *RouteDecision
	Upstream  *UpstreamOutcome
}
