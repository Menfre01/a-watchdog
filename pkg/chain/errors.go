package chain

import (
	"errors"
	"fmt"
)

// 链组装的哨兵错误(供调用方以 errors.Is 判断)。
var (
	// ErrUnknownMiddleware 表示配置引用了未注册的中间件名字。
	ErrUnknownMiddleware = errors.New("chain: unknown middleware")
	// ErrUnknownStage 表示注册了不受支持的阶段。
	ErrUnknownStage = errors.New("chain: unknown stage")
	// ErrDuplicateMiddleware 表示重复注册同名中间件。
	ErrDuplicateMiddleware = errors.New("chain: duplicate middleware")
	// ErrEmptyMiddlewareName 表示注册或引用空名字。
	ErrEmptyMiddlewareName = errors.New("chain: empty middleware name")
	// ErrNilTerminus 表示组装链时缺少终结点。
	ErrNilTerminus = errors.New("chain: nil terminus")
)

// RejectError 是一次受控的拒绝响应:中间件返回它即短路,
// 其后的处理环节(路由/调用)不再执行;响应体已按入向协议序列化,由运行时写出。
// Kind 用于请求记录归因(如 "auth" / "route_not_found")。
type RejectError struct {
	StatusCode  int
	ContentType string
	Body        []byte
	Kind        string
}

// Error 实现 error 接口。
func (e *RejectError) Error() string {
	return fmt.Sprintf("chain: rejected status=%d kind=%q", e.StatusCode, e.Kind)
}
