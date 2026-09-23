package chain

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// fakeMiddleware 是记录执行轨迹的测试中间件:
// 进入时记 "enter:<name>",退出时记 "exit:<name>";run 非空时以其为处理逻辑。
type fakeMiddleware struct {
	name  string
	stage Stage
	log   *[]string
	run   func(ctx *Context, next Handler) error
}

// Name 实现 Middleware。
func (f *fakeMiddleware) Name() string { return f.name }

// Stage 实现 Middleware。
func (f *fakeMiddleware) Stage() Stage { return f.stage }

// Handle 实现 Middleware。
func (f *fakeMiddleware) Handle(ctx *Context, next Handler) error {
	*f.log = append(*f.log, "enter:"+f.name)
	var err error
	if f.run != nil {
		err = f.run(ctx, next)
	} else {
		err = next(ctx)
	}
	*f.log = append(*f.log, "exit:"+f.name)
	return err
}

// fakeRegistry 为每个 fakeMiddleware 注册构造器,返回其注册表。
func fakeRegistry(mws ...*fakeMiddleware) *Registry {
	reg := NewRegistry()
	for _, mw := range mws {
		m := mw
		// 测试辅助:构造必然成功,忽略注册错误无风险。
		_ = reg.Register(m.name, m.stage, func(string, map[string]string) (Middleware, error) {
			return m, nil
		})
	}
	return reg
}

// enabled 生成一个「启用」的组装配置。
func enabled(name string) MiddlewareConfig {
	return MiddlewareConfig{Name: name, Enabled: true}
}

func TestBuild_StageOrderAndFinalizer(t *testing.T) {
	var log []string
	auth := &fakeMiddleware{name: "auth", stage: StageAdmission, log: &log}
	route := &fakeMiddleware{name: "route", stage: StageRouting, log: &log}
	record := &fakeMiddleware{name: "record", stage: StagePostprocess, log: &log}
	terminus := func(ctx *Context) error {
		log = append(log, "terminus")
		return nil
	}

	// 配置顺序故意打乱:阶段序由框架固定,不受配置顺序影响。
	h, err := Build(fakeRegistry(auth, route, record), []MiddlewareConfig{
		enabled("route"), enabled("record"), enabled("auth"),
	}, terminus)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if err := h(&Context{}); err != nil {
		t.Fatalf("chain run error = %v", err)
	}

	want := []string{
		"enter:record", // 响应后处理是最外层(finalizer 语义)
		"enter:auth",   // 准入先于路由
		"enter:route",
		"terminus",
		"exit:route",
		"exit:auth",
		"exit:record", // 主要工作发生在 next 返回之后
	}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Fatalf("execution order mismatch (-want +got):\n%s", diff)
	}
}

func TestBuild_DisabledMiddlewareNotExecuted(t *testing.T) {
	var log []string
	auth := &fakeMiddleware{name: "auth", stage: StageAdmission, log: &log}
	route := &fakeMiddleware{name: "route", stage: StageRouting, log: &log}
	terminus := func(ctx *Context) error {
		log = append(log, "terminus")
		return nil
	}

	cfg := []MiddlewareConfig{
		{Name: "auth", Enabled: false},
		enabled("route"),
	}
	h, err := Build(fakeRegistry(auth, route), cfg, terminus)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if err := h(&Context{}); err != nil {
		t.Fatalf("chain run error = %v", err)
	}

	want := []string{"enter:route", "terminus", "exit:route"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Fatalf("execution order mismatch (-want +got):\n%s", diff)
	}
}

func TestBuild_ShortCircuitSkipsLaterStages(t *testing.T) {
	var log []string
	rej := &RejectError{StatusCode: 401, ContentType: "application/json", Body: []byte(`{}`), Kind: "auth"}
	auth := &fakeMiddleware{
		name: "auth", stage: StageAdmission, log: &log,
		run: func(ctx *Context, next Handler) error { return rej },
	}
	route := &fakeMiddleware{name: "route", stage: StageRouting, log: &log}
	record := &fakeMiddleware{name: "record", stage: StagePostprocess, log: &log}
	terminus := func(ctx *Context) error {
		log = append(log, "terminus")
		return nil
	}

	h, err := Build(fakeRegistry(auth, route, record), []MiddlewareConfig{
		enabled("auth"), enabled("route"), enabled("record"),
	}, terminus)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	err = h(&Context{})
	var got *RejectError
	if !errors.As(err, &got) || got.StatusCode != 401 {
		t.Fatalf("chain error = %v, want *RejectError(401)", err)
	}

	// 路由与调用不执行;响应后处理仍执行(finalizer)。
	want := []string{"enter:record", "enter:auth", "exit:auth", "exit:record"}
	if diff := cmp.Diff(want, log); diff != "" {
		t.Fatalf("execution order mismatch (-want +got):\n%s", diff)
	}
}

func TestBuild_UnknownMiddlewareRejected(t *testing.T) {
	terminus := func(ctx *Context) error { return nil }
	reg := fakeRegistry()

	// 启用或停用,未注册的名字都必须报错。
	for _, cfg := range [][]MiddlewareConfig{
		{{Name: "nope", Enabled: true}},
		{{Name: "nope", Enabled: false}},
	} {
		h, err := Build(reg, cfg, terminus)
		if !errors.Is(err, ErrUnknownMiddleware) {
			t.Fatalf("Build() error = %v, want ErrUnknownMiddleware", err)
		}
		if h != nil {
			t.Fatalf("Build() handler = non-nil, want nil on error")
		}
	}
}

func TestBuild_NilTerminusRejected(t *testing.T) {
	if _, err := Build(fakeRegistry(), nil, nil); !errors.Is(err, ErrNilTerminus) {
		t.Fatalf("Build() error = %v, want ErrNilTerminus", err)
	}
}

func TestBuild_ParamsPassedToConstructor(t *testing.T) {
	var got map[string]string
	reg := NewRegistry()
	err := reg.Register("with-params", StageAdmission, func(name string, params map[string]string) (Middleware, error) {
		got = params
		var log []string
		return &fakeMiddleware{name: name, stage: StageAdmission, log: &log}, nil
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	params := map[string]string{"granularity": "fine"}
	h, err := Build(reg, []MiddlewareConfig{
		{Name: "with-params", Enabled: true, Params: params},
	}, func(ctx *Context) error { return nil })
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if err := h(&Context{}); err != nil {
		t.Fatalf("chain run error = %v", err)
	}
	if diff := cmp.Diff(params, got); diff != "" {
		t.Fatalf("constructor params mismatch (-want +got):\n%s", diff)
	}
}
