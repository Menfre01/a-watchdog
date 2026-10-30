package middleware

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ds-context/pkg/chain"
	"ds-context/pkg/config"
	"ds-context/pkg/config/db"
	"ds-context/pkg/trace"
)

// fakeStore 是可控的 InsertStore 假实现。
type fakeStore struct {
	block     chan struct{} // 非 nil 时每次插入先等待
	delay     time.Duration // 每次插入的模拟耗时
	fail      bool
	failTrace bool // SaveTrace 失败(验证轨迹失败不影响记录)
	failSeq   bool // NextSnapshotSeq 失败(验证快照失败只落原始层)
	shortIDs  bool // 返回的 id 数少于入参数(验证放弃本批轨迹的守卫)
	attempts  atomic.Int64
	mu        sync.Mutex
	got       []db.InsertRequestParams
	traces    []db.InsertRequestBodyParams
	snaps     []*config.TraceSnapshot
	seqs      map[string]int64
	nextID    int64
}

// InsertRequests 实现 InsertStore:返回与入参一一对应的自增 id。
func (f *fakeStore) InsertRequests(_ context.Context, reqs []db.InsertRequestParams) ([]int64, error) {
	f.attempts.Add(1)
	if f.block != nil {
		<-f.block
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.fail {
		return nil, errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, reqs...)
	ids := make([]int64, 0, len(reqs))
	for range reqs {
		f.nextID++
		ids = append(ids, f.nextID)
	}
	if f.shortIDs && len(ids) > 0 {
		ids = ids[:len(ids)-1]
	}
	return ids, nil
}

// count 返回已写入记录数。
func (f *fakeStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

// SaveTrace 实现 InsertStore:记录轨迹写入参数,便于断言原始层内容。
func (f *fakeStore) SaveTrace(_ context.Context, body db.InsertRequestBodyParams, snap *config.TraceSnapshot) error {
	if f.failTrace {
		return errors.New("trace boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.traces = append(f.traces, body)
	f.snaps = append(f.snaps, snap)
	return nil
}

// NextSnapshotSeq 实现 InsertStore:按会话递增。
func (f *fakeStore) NextSnapshotSeq(_ context.Context, sessionID string) (int64, error) {
	if f.failSeq {
		return 0, errors.New("seq boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seqs == nil {
		f.seqs = map[string]int64{}
	}
	f.seqs[sessionID]++
	return f.seqs[sessionID], nil
}

// lastSnapshot 返回最后一条轨迹对应的快照(可能为 nil)。
func (f *fakeStore) lastSnapshot() *config.TraceSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.snaps) == 0 {
		return nil
	}
	return f.snaps[len(f.snaps)-1]
}

// traceCount 返回已写入轨迹数。
func (f *fakeStore) traceCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.traces)
}

// lastTrace 返回最后一条轨迹(调用前需确认 traceCount() > 0)。
func (f *fakeStore) lastTrace() db.InsertRequestBodyParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.traces[len(f.traces)-1]
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
		queue:  make(chan recordItem, recordQueueSize),
		logger: discardLogger(),
	}

	// 超过单批上限的积压:应分批写完全部记录。
	total := recordBatchMax + 3
	for i := 0; i < total; i++ {
		rec.queue <- recordItem{params: db.InsertRequestParams{Ts: "t", Protocol: "p", StatusCode: 200, LatencyMs: 1}}
	}
	rec.drain()
	if fs.count() != total {
		t.Fatalf("drain wrote %d records, want %d", fs.count(), total)
	}
}

// TestRecorder_WritesRawLayer 覆盖原始层落库:请求行、脱敏后的入向头与可解压的请求体。
func TestRecorder_WritesRawLayer(t *testing.T) {
	fs := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	req := httptest.NewRequestWithContext(ctx, http.MethodPost,
		"/v1/messages?beta=true&access_token=SECRET", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("X-Claude-Code-Session-Id", "sess-1")
	req.Header.Set("Authorization", "Bearer sk-secret")
	c := &chain.Context{
		R:         req,
		Protocol:  "anthropic-messages",
		StartedAt: time.Now(),
		Body:      []byte(`{"model":"m"}`),
	}
	if err := rec.Handle(c, func(*chain.Context) error { return nil }); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	eventually(t, func() bool { return fs.traceCount() == 1 })

	got := fs.lastTrace()
	if got.Method != http.MethodPost || got.Path != "/v1/messages" {
		t.Errorf("请求行 = %s %s?%s", got.Method, got.Path, got.Query)
	}
	// 查询串的凭据同样不可再生:敏感参数必须脱敏,其余原样保留。
	if !strings.Contains(got.Query, "beta=true") {
		t.Errorf("查询串丢失非敏感参数: %q", got.Query)
	}
	if strings.Contains(got.Query, "SECRET") {
		t.Errorf("查询串凭据未脱敏: %q", got.Query)
	}
	body, err := trace.Decompress(got.BodyGzip)
	if err != nil {
		t.Fatalf("解压请求体失败: %v", err)
	}
	if string(body) != `{"model":"m"}` {
		t.Errorf("请求体往返 = %s", body)
	}
	if strings.Contains(got.Headers, "sk-secret") {
		t.Errorf("凭据未脱敏: %s", got.Headers)
	}
	if !strings.Contains(got.Headers, "sess-1") || !strings.Contains(got.Headers, "[redacted]") {
		t.Errorf("头落盘不符: %s", got.Headers)
	}
}

// TestRecorder_TraceFailureKeepsRecord 覆盖失败隔离:轨迹写入失败时记录仍落库。
func TestRecorder_TraceFailureKeepsRecord(t *testing.T) {
	fs := &fakeStore{failTrace: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	if err := rec.Handle(&chain.Context{Protocol: "anthropic-messages", StartedAt: time.Now()},
		func(*chain.Context) error { return nil }); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	eventually(t, func() bool { return fs.count() == 1 })
	if fs.traceCount() != 0 {
		t.Fatalf("轨迹写入失败却记录了轨迹: %d", fs.traceCount())
	}
}

// TestRecorder_SkipsTraceOnIDCountMismatch 覆盖关联守卫:id 数与元素数不一致时
// 放弃本批轨迹,避免把轨迹挂到错误的请求上;记录本身已提交,不回滚。
func TestRecorder_SkipsTraceOnIDCountMismatch(t *testing.T) {
	fs := &fakeStore{shortIDs: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	if err := rec.Handle(&chain.Context{Protocol: "anthropic-messages", StartedAt: time.Now()},
		func(*chain.Context) error { return nil }); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	eventually(t, func() bool { return fs.count() == 1 })
	if fs.traceCount() != 0 {
		t.Fatalf("id 数不符却写了轨迹: %d", fs.traceCount())
	}
}

// TestRecorder_TraceUsesReturnedIDs 覆盖 id 关联:轨迹的 request_id 取自记录写入返回的 id。
func TestRecorder_TraceUsesReturnedIDs(t *testing.T) {
	fs := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	const n = 3
	for i := 0; i < n; i++ {
		if err := rec.Handle(&chain.Context{Protocol: "openai-chat", StartedAt: time.Now()},
			func(*chain.Context) error { return nil }); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}
	eventually(t, func() bool { return fs.traceCount() == n })

	fs.mu.Lock()
	defer fs.mu.Unlock()
	for i, tr := range fs.traces {
		if tr.RequestID != int64(i+1) {
			t.Errorf("第 %d 条轨迹 request_id = %d, want %d", i, tr.RequestID, i+1)
		}
	}
}

// TestRecorder_WritesSnapshot 覆盖快照生成:会话归属来自判定,ndjson 可解压,派生列齐备。
func TestRecorder_WritesSnapshot(t *testing.T) {
	fs := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("x-claude-code-session-id", "sess-1")
	c := &chain.Context{
		R:         req,
		Protocol:  "anthropic-messages",
		StartedAt: time.Now(),
		Body:      []byte(body),
		ClientKey: &chain.ClientIdentity{KeyID: 7, KeyName: "k7"},
	}
	if err := rec.Handle(c, func(*chain.Context) error { return nil }); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	eventually(t, func() bool { return fs.traceCount() == 1 })

	snap := fs.lastSnapshot()
	if snap == nil {
		t.Fatal("未生成快照")
	}
	if snap.SessionID != "7:sess-1" || snap.SessionSource != "explicit" {
		t.Errorf("会话归属 = %q/%q, want 7:sess-1/explicit", snap.SessionID, snap.SessionSource)
	}
	if !strings.Contains(snap.SessionNote, "x-claude-code-session-id") {
		t.Errorf("判定理由 = %q", snap.SessionNote)
	}
	if snap.Seq != 1 {
		t.Errorf("Seq = %d, want 1", snap.Seq)
	}
	ndjson, err := trace.Decompress(snap.NdjsonGzip)
	if err != nil {
		t.Fatalf("解压快照失败: %v", err)
	}
	if !strings.Contains(string(ndjson), `"role":"user"`) {
		t.Errorf("快照内容 = %s", ndjson)
	}
	if len(snap.LineHashes) != 8 || len(snap.LineSizes) != 4 {
		t.Errorf("派生列长度 = %d/%d, want 8/4(单行)", len(snap.LineHashes), len(snap.LineSizes))
	}
}

// TestRecorder_SkipsSnapshotForAuxEndpoint 覆盖辅助端点:只落原始层,不产快照。
func TestRecorder_SkipsSnapshotForAuxEndpoint(t *testing.T) {
	fs := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(body))
	c := &chain.Context{R: req, Protocol: "anthropic-messages", StartedAt: time.Now(), Body: []byte(body)}
	if err := rec.Handle(c, func(*chain.Context) error { return nil }); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	eventually(t, func() bool { return fs.traceCount() == 1 })
	if snap := fs.lastSnapshot(); snap != nil {
		t.Errorf("辅助端点不应产快照: %+v", snap)
	}
}

// TestRecorder_SkipsSnapshotForUnparsableBody 覆盖请求体不可解析:只落原始层。
func TestRecorder_SkipsSnapshotForUnparsableBody(t *testing.T) {
	fs := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	body := `not json`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/messages", strings.NewReader(body))
	c := &chain.Context{R: req, Protocol: "anthropic-messages", StartedAt: time.Now(), Body: []byte(body)}
	if err := rec.Handle(c, func(*chain.Context) error { return nil }); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	eventually(t, func() bool { return fs.traceCount() == 1 })
	if snap := fs.lastSnapshot(); snap != nil {
		t.Errorf("不可解析请求体不应产快照: %+v", snap)
	}
}

// TestRecorder_SnapshotFailureKeepsRawLayer 覆盖捕捉步骤失败的隔离:
// 会话序号查询失败时该请求仍落原始层,且不影响请求与请求记录。
func TestRecorder_SnapshotFailureKeepsRawLayer(t *testing.T) {
	fs := &fakeStore{failSeq: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/messages", strings.NewReader(body))
	c := &chain.Context{R: req, Protocol: "anthropic-messages", StartedAt: time.Now(), Body: []byte(body)}
	if err := rec.Handle(c, func(*chain.Context) error { return nil }); err != nil {
		t.Fatalf("Handle() error = %v, want nil(捕捉失败不影响请求)", err)
	}
	eventually(t, func() bool { return fs.traceCount() == 1 })
	if snap := fs.lastSnapshot(); snap != nil {
		t.Errorf("序号获取失败时不应产快照: %+v", snap)
	}
	if fs.count() != 1 {
		t.Errorf("请求记录数 = %d, want 1", fs.count())
	}
}

// TestRecorder_SkipsSnapshotForEmptyMessages 覆盖空 messages:没有可观测内容,不落快照。
func TestRecorder_SkipsSnapshotForEmptyMessages(t *testing.T) {
	fs := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := NewRecorder(ctx, fs, discardLogger())

	body := `{"model":"m"}` // 无 messages:如仅探测模型可用性
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/messages", strings.NewReader(body))
	c := &chain.Context{R: req, Protocol: "anthropic-messages", StartedAt: time.Now(), Body: []byte(body)}
	if err := rec.Handle(c, func(*chain.Context) error { return nil }); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	eventually(t, func() bool { return fs.traceCount() == 1 })
	if snap := fs.lastSnapshot(); snap != nil {
		t.Errorf("空 messages 不应产快照: %+v", snap)
	}
}
