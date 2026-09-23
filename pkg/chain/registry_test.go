package chain

import (
	"errors"
	"testing"
)

func TestRegistry_RegisterValidation(t *testing.T) {
	tests := []struct {
		name    string
		mwName  string
		stage   Stage
		ctor    Constructor
		wantErr error
	}{
		{"空名字", "", StageAdmission, func(string, map[string]string) (Middleware, error) { return nil, nil }, ErrEmptyMiddlewareName},
		{"未知阶段", "x", Stage("nope"), func(string, map[string]string) (Middleware, error) { return nil, nil }, ErrUnknownStage},
		{"空构造函数", "y", StageAdmission, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewRegistry()
			err := reg.Register(tt.mwName, tt.stage, tt.ctor)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Register() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err == nil {
				t.Fatalf("Register() error = nil, want non-nil")
			}
		})
	}
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	reg := NewRegistry()
	ctor := func(string, map[string]string) (Middleware, error) { return nil, nil }
	if err := reg.Register("dup", StageAdmission, ctor); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if err := reg.Register("dup", StageRouting, ctor); !errors.Is(err, ErrDuplicateMiddleware) {
		t.Fatalf("second Register() error = %v, want ErrDuplicateMiddleware", err)
	}
}
