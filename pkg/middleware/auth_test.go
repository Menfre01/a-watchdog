package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"awatchdog/pkg/chain"
	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
)

// testClientKey 是测试用客户端密钥明文。
const testClientKey = "awd_test_key_abcdef"

// newStore 打开临时配置库(测试辅助)。
func newStore(t *testing.T) *config.Store {
	t.Helper()
	s, err := config.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("config.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return s
}

// newCtx 构造测试上下文(可带请求头与请求体)。
func newCtx(protocolName string, headers map[string]string, body string) *chain.Context {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return &chain.Context{Protocol: protocolName, R: req, Body: []byte(body), StartedAt: time.Now()}
}

// okHandler 返回记录调用与否的下游 handler。
func okHandler(called *bool) chain.Handler {
	return func(*chain.Context) error {
		*called = true
		return nil
	}
}

// createTestKey 在库中创建可用的测试密钥。
func createTestKey(t *testing.T, s *config.Store) {
	t.Helper()
	if _, err := s.CreateClientKey(context.Background(), db.CreateClientKeyParams{
		Name: "cli", KeyHash: config.HashClientKey(testClientKey), KeyPrefix: "awd_te", Enabled: 1,
	}); err != nil {
		t.Fatalf("CreateClientKey() error = %v", err)
	}
}

func TestAuth_CredentialCarriers(t *testing.T) {
	store := newStore(t)
	createTestKey(t, store)
	mw := NewAuth(store.Snapshot)

	tests := []struct {
		name    string
		headers map[string]string
		wantOK  bool
	}{
		{"Bearer 通过", map[string]string{"Authorization": "Bearer " + testClientKey}, true},
		{"x-api-key 通过", map[string]string{"x-api-key": testClientKey}, true},
		{"无凭据拒绝", nil, false},
		{"无效 key 拒绝", map[string]string{"Authorization": "Bearer wrong-key"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			ctx := newCtx("anthropic-messages", tt.headers, `{}`)
			err := mw.Handle(ctx, okHandler(&called))
			if tt.wantOK {
				if err != nil || !called {
					t.Fatalf("err = %v, called = %v", err, called)
				}
				if ctx.ClientKey == nil || ctx.ClientKey.KeyName != "cli" || ctx.ClientKey.KeyID == 0 {
					t.Fatalf("identity = %+v", ctx.ClientKey)
				}
				return
			}
			var rejected *chain.RejectError
			if !errors.As(err, &rejected) || rejected.StatusCode != http.StatusUnauthorized {
				t.Fatalf("err = %v, want 401 RejectError", err)
			}
			if called {
				t.Fatalf("下游不应被调用")
			}
		})
	}
}

func TestAuth_RejectionShapesPerProtocol(t *testing.T) {
	store := newStore(t)
	mw := NewAuth(store.Snapshot)

	ctx := newCtx("anthropic-messages", nil, `{}`)
	var rejected *chain.RejectError
	if err := mw.Handle(ctx, okHandler(new(bool))); !errors.As(err, &rejected) {
		t.Fatalf("err = %v, want RejectError", err)
	}
	if !strings.Contains(string(rejected.Body), "authentication_error") {
		t.Fatalf("anthropic 错误体 = %s", rejected.Body)
	}

	ctx2 := newCtx("openai-chat", nil, `{}`)
	if err := mw.Handle(ctx2, okHandler(new(bool))); !errors.As(err, &rejected) {
		t.Fatalf("err = %v, want RejectError", err)
	}
	if !strings.Contains(string(rejected.Body), "invalid_request_error") {
		t.Fatalf("openai 错误体 = %s", rejected.Body)
	}
}

func TestAuth_DisabledKeyRejected(t *testing.T) {
	store := newStore(t)
	createTestKey(t, store)
	keys, err := store.ListClientKeys(context.Background())
	if err != nil || len(keys) != 1 {
		t.Fatalf("ListClientKeys() = %v, %v", keys, err)
	}
	if err := store.SetClientKeyEnabled(context.Background(), keys[0].ID, false); err != nil {
		t.Fatalf("SetClientKeyEnabled() error = %v", err)
	}
	mw := NewAuth(store.Snapshot)
	called := false
	err = mw.Handle(newCtx("openai-chat", map[string]string{"Authorization": "Bearer " + testClientKey}, `{}`), okHandler(&called))
	var rejected *chain.RejectError
	if !errors.As(err, &rejected) || rejected.StatusCode != http.StatusUnauthorized || called {
		t.Fatalf("err = %v, called = %v; want 401 且不进入下游", err, called)
	}
}
