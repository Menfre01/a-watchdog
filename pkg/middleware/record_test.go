package middleware

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ds-context/pkg/chain"
	"ds-context/pkg/config/db"
)

// fakeStore 是可控的 InsertStore 假实现。
type fakeStore struct {
	block    chan struct{} // 非 nil 时每次插入先等待
	delay    time.Duration // 每次插入的模拟耗时
	fail     bool
	attempts atomic.Int64
	mu       sync.Mutex
	got      []db.InsertRequestParams
}

// InsertRequests 实现 InsertStore。
func (f *fakeStore) InsertRequests(_ context.Context, reqs []db.InsertRequestParams) error {
	f.attempts.Add(1)
	if f.block != nil {
		<-f.block
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.fail {
		return errors.New("boom")
	}
	f.mu.Lock()
	f.got = append(f.got, reqs...)
	f.mu.Unlock()
	return nil
}

// count 返回已写入记录数。
func (f *fakeStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

// discardLogger 返回丢弃输出的 slog。
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// eventually 轮询等待条件成立(异步写入的测试同步点)。
func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("条件在超时前未成立")
}

func TestRecorder_WritesSuccessRecord(t *testing.T) {
	ctxbg := context.Background()
	store := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rec := NewRecorder(ctx, store, discardLogger())

	up, out := int64(10), int64(20)
	c := &chain.Context{
		Protocol:  "anthropic-messages",
		StartedAt: time.Now(),
		ClientKey: &chain.ClientIdentity{KeyID: 1, KeyName: "cli"},
		Route: &chain.RouteDecision{
			ClientModel: "m", ProviderID: 2, UpstreamModel: "um",
		},
	}
	err := rec.Handle(c, func(ctx *chain.Context) error {
		ctx.Upstream = &chain.UpstreamOutcome{StatusCode: 200, Stream: true, UsageIn: &up, UsageOut: &out}
		return nil
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	eventually(t, func() bool {
		n, cerr := store.CountRequests(ctxbg)
		return cerr == nil && n == 1
	})
	rows, err := store.ListRecentRequests(ctxbg, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListRecentRequests() = %v, %v", rows, err)
	}
	r := rows[0]
	if r.StatusCode != 200 || r.Stream != 1 || r.Protocol != "anthropic-messages" ||
		r.RequestedModel != "m" || r.UpstreamModel == nil || *r.UpstreamModel != "um" ||
		r.ClientKeyName != "cli" || r.ErrorKind != nil || r.LatencyMs < 0 {
		t.Fatalf("record = %+v", r)
	}
	if r.UsageIn == nil || *r.UsageIn != 10 || r.UsageOut == nil || *r.UsageOut != 20 {
		t.Fatalf("record usage = in:%v out:%v", r.UsageIn, r.UsageOut)
	}
}

func TestRecorder_RecordsRejectedRequests(t *testing.T) {
	ctxbg := context.Background()
	store := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rec := NewRecorder(ctx, store, discardLogger())

	rejected := &chain.RejectError{StatusCode: http.StatusUnauthorized, ContentType: "application/json", Body: []byte(`{}`), Kind: "auth"}
	err := rec.Handle(&chain.Context{Protocol: "openai-chat", StartedAt: time.Now()},
		func(*chain.Context) error { return rejected })
	var got *chain.RejectError
	if !errors.As(err, &got) || got != rejected {
		t.Fatalf("Handle() error = %v, want 原样返回 RejectError", err)
	}

	eventually(t, func() bool {
		n, cerr := store.CountRequests(ctxbg)
		return cerr == nil && n == 1
	})
	rows, err := store.ListRecentRequests(ctxbg, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListRecentRequests() = %v, %v", rows, err)
	}
	if r := rows[0]; r.StatusCode != 401 || r.ErrorKind == nil || *r.ErrorKind != "auth" {
		t.Fatalf("record = %+v", rows[0])
	}
}

func TestRecorder_DoesNotBlockOnFullQueue(t *testing.T) {
	fs := &fakeStore{block: make(chan struct{})}
	defer close(fs.block)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	start := time.Now()
	for i := 0; i < recordQueueSize*2; i++ {
		c := &chain.Context{Protocol: "anthropic-messages", StartedAt: time.Now()}
		if err := rec.Handle(c, func(*chain.Context) error { return nil }); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("入队耗时 %v:记录不应阻塞请求", elapsed)
	}
	if dropped := rec.Dropped(); dropped == 0 {
		t.Fatalf("队列溢出后 Dropped() = 0, want > 0")
	}
}

func TestRecorder_WriteFailureDoesNotAffectRequest(t *testing.T) {
	fs := &fakeStore{fail: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	err := rec.Handle(&chain.Context{Protocol: "openai-chat", StartedAt: time.Now()},
		func(*chain.Context) error { return nil })
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil(写入失败不影响请求)", err)
	}
	eventually(t, func() bool { return fs.attempts.Load() > 0 })
	if fs.count() != 0 {
		t.Fatalf("失败写入不应落库")
	}
}

func TestRecorder_DrainsQueueOnShutdown(t *testing.T) {
	// 关停时队列中的剩余记录必须在退出前被写掉。
	fs := &fakeStore{delay: 20 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	rec := NewRecorder(ctx, fs, discardLogger())

	for i := 0; i < 5; i++ {
		c := &chain.Context{Protocol: "anthropic-messages", StartedAt: time.Now()}
		if err := rec.Handle(c, func(*chain.Context) error { return nil }); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}
	cancel() // 立即触发关停:余下记录应由 drain 落库。

	eventually(t, func() bool { return fs.count() == 5 })
}

func TestRecorder_DrainBatchLimit(t *testing.T) {
	// 直接构造(不启动 run 协程),避免与后台消费竞争,确定性覆盖 drain 的分批分支。
	fs := &fakeStore{}
	rec := &Recorder{
		store:  fs,
		queue:  make(chan db.InsertRequestParams, recordQueueSize),
		logger: discardLogger(),
	}

	// 超过单批上限的积压:应分批写完全部记录。
	total := recordBatchMax + 3
	for i := 0; i < total; i++ {
		rec.queue <- db.InsertRequestParams{Ts: "t", Protocol: "p", StatusCode: 200, LatencyMs: 1}
	}
	rec.drain()
	if fs.count() != total {
		t.Fatalf("drain wrote %d records, want %d", fs.count(), total)
	}
}
