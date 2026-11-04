package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"awatchdog/pkg/chain"
	"awatchdog/pkg/trace"
)

// responseCount 返回已写入的响应原文条数。
func (f *fakeStore) responseCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.responses)
}

// TestRecorder_WritesResponseBody 覆盖响应原文落库:
// 压缩后可还原,截断与完整性标记随记录一起写入。
func TestRecorder_WritesResponseBody(t *testing.T) {
	fs := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	const respBody = `{"usage":{"prompt_tokens":1}}`
	if err := rec.Handle(&chain.Context{Protocol: "openai-chat", StartedAt: time.Now()},
		func(c *chain.Context) error {
			c.ResponseBody = []byte(respBody)
			c.ResponseTruncated = true
			return nil
		}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	eventually(t, func() bool { return fs.responseCount() > 0 })
	fs.mu.Lock()
	got := fs.responses[0]
	fs.mu.Unlock()
	if got.RequestID != 1 {
		t.Errorf("RequestID = %d, want 1", got.RequestID)
	}
	body, err := trace.Decompress(got.BodyGzip)
	if err != nil {
		t.Fatalf("解压响应失败: %v", err)
	}
	if string(body) != respBody {
		t.Errorf("响应往返 = %s", body)
	}
	if got.Truncated != 1 || got.Complete != 1 {
		t.Errorf("截断/完整性 = %d/%d, want 1/1", got.Truncated, got.Complete)
	}
}

// TestRecorder_ResponseWriteFailureDoesNotAffectRequest 覆盖响应写入失败的隔离:
// 只落日志,请求与记录本身不受影响。
func TestRecorder_ResponseWriteFailureDoesNotAffectRequest(t *testing.T) {
	fs := &fakeStore{failResp: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	err := rec.Handle(&chain.Context{Protocol: "openai-chat", StartedAt: time.Now()},
		func(c *chain.Context) error {
			c.ResponseBody = []byte("body")
			return nil
		})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	eventually(t, func() bool { return fs.count() == 1 })
	if fs.responseCount() != 0 {
		t.Errorf("响应写入失败却记录了 %d 条响应", fs.responseCount())
	}
}

// TestRecorder_SkipsSnapshotOnOversizedLine 覆盖快照序列化失败:
// 单行超过上限时只落原始层,不产残缺快照。
func TestRecorder_SkipsSnapshotOnOversizedLine(t *testing.T) {
	if testing.Short() {
		t.Skip("需要构造数兆字节请求体,短模式跳过")
	}
	fs := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	// json.Marshal 会把 & 转义为 \u0026(6 字节),12 MiB 文本足以超过 64 MiB 行上限。
	body := `{"model":"m","messages":[{"role":"user","content":"` +
		strings.Repeat("&", 12<<20) + `"}]}`
	// 路径必须是会话型端点,否则会在解析前直接跳过快照(覆盖不到序列化失败)。
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/messages", nil)
	c := &chain.Context{
		R:         req,
		Protocol:  "anthropic-messages",
		StartedAt: time.Now(),
		Body:      []byte(body),
	}
	if err := rec.Handle(c, func(*chain.Context) error { return nil }); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	eventually(t, func() bool { return fs.traceCount() == 1 })
	if snap := fs.lastSnapshot(); snap != nil {
		t.Errorf("超长行不应产快照: %+v", snap)
	}
	if fs.count() != 1 {
		t.Errorf("请求记录数 = %d, want 1(原始层仍落库)", fs.count())
	}
}
