package chain

import (
	"errors"
	"strings"
	"testing"
)

func TestRejectError_ErrorMessage(t *testing.T) {
	e := &RejectError{StatusCode: 401, Kind: "auth"}
	msg := e.Error()
	if !strings.Contains(msg, "401") || !strings.Contains(msg, "auth") {
		t.Fatalf("Error() = %q", msg)
	}
}

func TestBuild_ConstructorError(t *testing.T) {
	ctorErr := errors.New("boom")
	reg := NewRegistry()
	if err := reg.Register("bad", StageAdmission, func(string, map[string]string) (Middleware, error) {
		return nil, ctorErr
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	_, err := Build(reg, []MiddlewareConfig{{Name: "bad", Enabled: true}}, func(*Context) error { return nil })
	if !errors.Is(err, ctorErr) {
		t.Fatalf("Build() error = %v, want ctorErr", err)
	}
}
