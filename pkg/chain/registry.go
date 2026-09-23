package chain

import "fmt"

// Constructor 由配置参数构造中间件实例;name 用于错误信息。
type Constructor func(name string, params map[string]string) (Middleware, error)

// Registry 是中间件的编译期注册表:名字唯一,注册即声明阶段与构造函数。
// 运行时(配置)只能启停注册表中已有的名字,不能引入新实现。
type Registry struct {
	entries map[string]registryEntry
}

// registryEntry 是注册表内的单条记录。
type registryEntry struct {
	stage Stage
	ctor  Constructor
}

// NewRegistry 创建空的中间件注册表。
func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]registryEntry)}
}

// Register 注册中间件:重名、未知阶段、空名字或空构造函数均返回错误。
func (r *Registry) Register(name string, stage Stage, ctor Constructor) error {
	if name == "" {
		return ErrEmptyMiddlewareName
	}
	if !knownStage(stage) {
		return fmt.Errorf("%w: %q(%q)", ErrUnknownStage, name, stage)
	}
	if ctor == nil {
		return fmt.Errorf("chain: middleware %q: nil constructor", name)
	}
	if _, dup := r.entries[name]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateMiddleware, name)
	}
	r.entries[name] = registryEntry{stage: stage, ctor: ctor}
	return nil
}

// lookup 返回注册记录;调用方应先通过 Build 的校验流程再使用。
func (r *Registry) lookup(name string) (registryEntry, bool) {
	e, ok := r.entries[name]
	return e, ok
}
