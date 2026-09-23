// Package chain 实现网关的责任链运行框架:固定阶段序、编译期注册的中间件、
// 强类型请求上下文与受控短路。
//
// 行为契约:openspec/changes/gateway-core/specs/request-pipeline/spec.md;
// 设计决策:design.md D3。
package chain

import "fmt"

// Stage 标识中间件所属的固定阶段。
type Stage string

const (
	// StageAdmission 准入阶段(鉴权等)。
	StageAdmission Stage = "admission"
	// StageRouting 路由/适配阶段(模型目录解析、上游选择)。
	StageRouting Stage = "routing"
	// StagePostprocess 响应后处理阶段:以 finalizer 语义环绕整条链,
	// 被短路或被拒绝的请求同样会经过(中间件应在 next 返回后再完成主要工作)。
	StagePostprocess Stage = "postprocess"
)

// stageExecutionOrder 是链的固定执行顺序(外层 → 内层):
// 响应后处理最先进入、最后退出;准入先于路由。
var stageExecutionOrder = []Stage{StagePostprocess, StageAdmission, StageRouting}

// knownStage 判断阶段值是否受支持。
func knownStage(s Stage) bool {
	for _, k := range stageExecutionOrder {
		if k == s {
			return true
		}
	}
	return false
}

// Handler 是链上的处理函数;中间件通过调用 next 推进链路。
type Handler func(ctx *Context) error

// Middleware 是链上的一个处理环节。
type Middleware interface {
	// Name 返回注册名(唯一标识该中间件)。
	Name() string
	// Stage 返回该环节所属的固定阶段。
	Stage() Stage
	// Handle 处理请求:调用 next 继续链路;返回 *RejectError 表示受控拒绝,
	// 返回其他错误表示内部故障——两者都会中止其后的处理环节。
	Handle(ctx *Context, next Handler) error
}

// MiddlewareConfig 是单个中间件的组装配置(启用与参数由配置控制)。
type MiddlewareConfig struct {
	Name    string
	Enabled bool
	Params  map[string]string
}

// Build 依配置组装责任链:按固定阶段序(响应后处理 → 准入 → 路由/适配)
// 把已启用的中间件环绕到终结点上;同阶段内保持配置顺序。
// 未注册的名字(含停用项)与空终结点在此显式报错,不静默跳过。
func Build(reg *Registry, cfgs []MiddlewareConfig, terminus Handler) (Handler, error) {
	if terminus == nil {
		return nil, ErrNilTerminus
	}
	// 先校验全部引用:未注册的名字必须显式报错(无论启用与否)。
	for _, cfg := range cfgs {
		if _, ok := reg.lookup(cfg.Name); !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownMiddleware, cfg.Name)
		}
	}
	// 按固定阶段序收集已启用实例;同阶段保持配置顺序。
	var ordered []Middleware
	for _, stage := range stageExecutionOrder {
		for _, cfg := range cfgs {
			if !cfg.Enabled {
				continue
			}
			entry, _ := reg.lookup(cfg.Name) // 已校验存在
			if entry.stage != stage {
				continue
			}
			mw, err := entry.ctor(cfg.Name, cfg.Params)
			if err != nil {
				return nil, fmt.Errorf("chain: construct %q: %w", cfg.Name, err)
			}
			ordered = append(ordered, mw)
		}
	}
	// 自内向外包装:ordered 的最后一个最靠近终结点,第一个是链的最外层。
	h := terminus
	for i := len(ordered) - 1; i >= 0; i-- {
		mw := ordered[i]
		next := h
		h = func(ctx *Context) error { return mw.Handle(ctx, next) }
	}
	return h, nil
}
