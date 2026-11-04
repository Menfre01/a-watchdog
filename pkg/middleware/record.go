package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"awatchdog/pkg/chain"
	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
	"awatchdog/pkg/protocol"
	"awatchdog/pkg/trace"
)

// Recorder 是响应后处理环节:无论请求是否被短路,都异步落一条请求记录与轨迹。
// finalizer 语义使其能观察被拒绝的请求(对应 request-logging spec)。
type Recorder struct {
	store   InsertStore
	queue   chan recordItem
	logger  *slog.Logger
	dropped atomic.Int64
}

// InsertStore 是记录与捕捉环节所需的最小写入依赖(config.Store 满足)。
type InsertStore interface {
	// InsertRequests 写入一批请求记录,返回与入参一一对应的自增 id。
	InsertRequests(ctx context.Context, reqs []db.InsertRequestParams) ([]int64, error)
	// SaveTrace 以单请求事务写入轨迹:原始层必写,快照可选。
	SaveTrace(ctx context.Context, body db.InsertRequestBodyParams, snap *config.TraceSnapshot) error
	// NextSnapshotSeq 返回某会话的下一个快照序号(自 1 起)。
	NextSnapshotSeq(ctx context.Context, sessionID string) (int64, error)
	// InsertResponseBody 写入上游响应原文(旁路副本)。
	InsertResponseBody(ctx context.Context, arg db.InsertResponseBodyParams) error
}

const (
	// recordQueueSize 是有界队列容量:队列元素自携带请求体原文后单元素量级升至 MB 级,
	// 故深度下调以保证内存有界(design.md D2);队满时丢弃并计数告警,永不阻塞请求。
	recordQueueSize = 128
	// recordBatchMax 是写协程单批合并的上限。
	recordBatchMax = 64
	// drainTimeout 是关停窗口内清空剩余记录的时间上限。
	drainTimeout = 2 * time.Second
)

// recordItem 是队列元素:请求记录参数 + 原始层所需数据。
// 请求体以未压缩原文携带(gzip 在写协程内做),入向头在入队前已脱敏。
type recordItem struct {
	params db.InsertRequestParams
	raw    rawRequest
	// session 在入队前判定:它只依赖请求头与 client key,与消息内容无关(design.md D4)。
	session trace.SessionDecision
	// resp 是上游响应原文副本;截断与完整性随记录一起落库。
	resp      []byte
	truncated bool
	complete  bool
}

// rawRequest 是原始层的入参:请求行、脱敏后的入向头与请求体原文。
type rawRequest struct {
	method  string
	path    string
	query   string
	headers string
	body    []byte
}

// NewRecorder 创建记录环节并启动后台写协程;ctx 取消时协程退出。
func NewRecorder(ctx context.Context, store InsertStore, logger *slog.Logger) *Recorder {
	r := &Recorder{
		store:  store,
		queue:  make(chan recordItem, recordQueueSize),
		logger: logger,
	}
	go r.run(ctx)
	return r
}

// Dropped 返回因队满被丢弃的记录数(观测用)。
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Name 实现 chain.Middleware。
func (r *Recorder) Name() string { return "record" }

// Stage 实现 chain.Middleware。
func (r *Recorder) Stage() chain.Stage { return chain.StagePostprocess }

// Handle 实现 chain.Middleware:先执行链,再异步落记录;绝不阻塞或改写结果。
func (r *Recorder) Handle(ctx *chain.Context, next chain.Handler) error {
	err := next(ctx)
	item := snapshotRecord(ctx, err)
	select {
	case r.queue <- item:
	default:
		n := r.dropped.Add(1)
		if n == 1 || n%100 == 0 {
			r.logger.Warn("请求记录队列已满,记录被丢弃", "dropped_total", n)
		}
	}
	return err
}

// run 是后台写协程:顺序消费队列,并在单批内尽量合并积压。
func (r *Recorder) run(ctx context.Context) {
	for {
		select {
		case first := <-r.queue:
			batch := []recordItem{first}
		collect:
			for len(batch) < recordBatchMax {
				select {
				case more := <-r.queue:
					batch = append(batch, more)
				default:
					break collect
				}
			}
			r.insert(ctx, batch)
		case <-ctx.Done():
			// 关停:尽力写掉队列剩余记录,避免丢失最后一批(独立短超时,不与已取消 ctx 绑定)。
			r.drain()
			return
		}
	}
}

// insert 写入一批记录与轨迹;任一步失败只落日志,绝不影响请求本身
// (对应 request-logging 与 trajectory-capture 规范)。
func (r *Recorder) insert(ctx context.Context, batch []recordItem) {
	// 关停窗口内 ctx 已被取消:改用独立短超时,否则本批写入必然失败(run 退出后
	// 队列不再有人消费,drain 会沦为摆设)。
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), drainTimeout)
		defer cancel()
	}
	reqs := make([]db.InsertRequestParams, len(batch))
	for i, item := range batch {
		reqs[i] = item.params
	}
	ids, err := r.store.InsertRequests(ctx, reqs)
	if err != nil {
		r.logger.Error("请求记录写入失败", "err", err)
		return
	}
	if len(ids) != len(batch) {
		// 记录已提交但 id 数不符:保守放弃本批轨迹,避免把轨迹挂到错误的请求上。
		r.logger.Error("记录 id 数与元素数不一致,跳过本批轨迹", "ids", len(ids), "items", len(batch))
		return
	}
	// 记录批事务已提交:逐请求独立事务写轨迹,单条失败只影响该请求的轨迹(design.md D2)。
	for i, item := range batch {
		body, err := buildRequestBody(ids[i], item.raw)
		if err != nil {
			// COVERAGE-EXEMPT: trace.Compress 只写 bytes.Buffer,错误分支不可达。
			r.logger.Error("轨迹压缩失败", "err", err)
			continue
		}
		snap, err := r.buildSnapshot(ctx, item)
		if err != nil {
			// 快照失败不丢原始层:请求体不可解析时只落原始层(design.md D2)。
			r.logger.Warn("快照生成失败,仅落原始层", "err", err)
		}
		if err := r.store.SaveTrace(ctx, body, snap); err != nil {
			r.logger.Error("轨迹写入失败", "err", err)
		}
		if len(item.resp) > 0 {
			compressed, err := trace.Compress(item.resp)
			if err != nil {
				// COVERAGE-EXEMPT: trace.Compress 只写 bytes.Buffer,错误分支不可达。
				r.logger.Error("响应压缩失败", "err", err)
				continue
			}
			if err := r.store.InsertResponseBody(ctx, db.InsertResponseBodyParams{
				RequestID: ids[i],
				BodyGzip:  compressed,
				Truncated: config.BoolToInt(item.truncated),
				Complete:  config.BoolToInt(item.complete),
			}); err != nil {
				r.logger.Error("响应写入失败", "err", err)
			}
		}
	}
}

// buildSnapshot 生成一次请求的规范化快照:解析消息、取会话序号、序列化并派生指纹。
// 返回 nil 表示该请求不产快照(辅助端点,或消息不可解析)。
func (r *Recorder) buildSnapshot(ctx context.Context, item recordItem) (*config.TraceSnapshot, error) {
	if !isConversationEndpoint(item.raw.path) {
		return nil, nil
	}
	messages, err := trace.Normalize(item.params.Protocol, item.raw.body)
	if err != nil {
		return nil, err
	}
	seq, err := r.store.NextSnapshotSeq(ctx, item.session.SessionID)
	if err != nil {
		return nil, err
	}
	serialized, err := trace.Serialize(messages)
	if err != nil {
		return nil, err
	}
	if serialized.Lines == 0 {
		// 空 messages(如仅探测模型可用性):没有可观测内容,不落快照。
		return nil, nil
	}
	compressed, err := trace.Compress(serialized.NDJSON)
	if err != nil {
		// COVERAGE-EXEMPT: trace.Compress 只写 bytes.Buffer,错误分支不可达。
		return nil, err
	}
	return &config.TraceSnapshot{
		SessionID:     item.session.SessionID,
		SessionSource: item.session.Source,
		SessionNote:   item.session.Note,
		Seq:           seq,
		Ts:            item.params.Ts,
		NdjsonGzip:    compressed,
		LineHashes:    serialized.LineHashes,
		LineSizes:     serialized.LineSizes,
	}, nil
}

// drain 在关停窗口内尽力清空队列。
func (r *Recorder) drain() {
	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	var batch []recordItem
	for {
		select {
		case p := <-r.queue:
			batch = append(batch, p)
			if len(batch) >= recordBatchMax {
				r.insert(ctx, batch)
				batch = batch[:0]
			}
		default:
			if len(batch) > 0 {
				r.insert(ctx, batch)
			}
			return
		}
	}
}

// snapshotRecord 把请求上下文与结果归集成一条队列元素(记录参数 + 原始层数据)。
func snapshotRecord(ctx *chain.Context, chainErr error) recordItem {
	params := db.InsertRequestParams{
		Ts:        ctx.StartedAt.UTC().Format(time.RFC3339Nano),
		Protocol:  ctx.Protocol,
		LatencyMs: time.Since(ctx.StartedAt).Milliseconds(),
	}
	if ctx.ClientKey != nil {
		keyID := ctx.ClientKey.KeyID
		params.ClientKeyID = &keyID
		params.ClientKeyName = ctx.ClientKey.KeyName
	}
	// 请求模型:优先取路由结果;被短路时尽力从请求体解析(仅用于记录)。
	if ctx.Route != nil {
		params.RequestedModel = ctx.Route.ClientModel
		providerID := ctx.Route.ProviderID
		params.ProviderID = &providerID
		upstreamModel := ctx.Route.UpstreamModel
		params.UpstreamModel = &upstreamModel
	} else if adapter, err := protocol.Get(ctx.Protocol); err == nil {
		if model, perr := adapter.ParseModel(ctx.Body); perr == nil {
			params.RequestedModel = model
		}
	}
	if ctx.Upstream != nil {
		params.StatusCode = int64(ctx.Upstream.StatusCode)
		params.Stream = config.BoolToInt(ctx.Upstream.Stream)
		params.UsageIn = ctx.Upstream.UsageIn
		params.UsageOut = ctx.Upstream.UsageOut
		params.UsageCache = ctx.Upstream.UsageCache
	}
	var rejected *chain.RejectError
	switch {
	case errors.As(chainErr, &rejected):
		params.StatusCode = int64(rejected.StatusCode)
		kind := rejected.Kind
		params.ErrorKind = &kind
	case chainErr != nil:
		if ctx.ResponseStarted && ctx.Upstream != nil {
			// 响应已开始后流中断(客户端断开/上游中途断):客户端实际收到的是上游状态,
			// 错误仅作归因,不覆盖真实状态码。
			kind := "stream_interrupted"
			params.ErrorKind = &kind
			break
		}
		params.StatusCode = http.StatusBadGateway
		kind := "internal"
		params.ErrorKind = &kind
	}
	var headers map[string][]string
	if ctx.R != nil {
		headers = ctx.R.Header
	}
	keyID := int64(0)
	if params.ClientKeyID != nil {
		keyID = *params.ClientKeyID
	}
	complete := chainErr == nil
	return recordItem{
		params:    params,
		raw:       rawRequestOf(ctx),
		session:   trace.ResolveSession(keyID, headers, ctx.StartedAt),
		resp:      ctx.ResponseBody,
		truncated: ctx.ResponseTruncated,
		complete:  complete,
	}
}

// conversationEndpoints 是产生快照的会话型推理端点。
// 辅助端点(如 count_tokens)只落原始层,不产快照(trajectory-capture 规范)。
var conversationEndpoints = map[string]bool{
	"/v1/messages":         true,
	"/v1/chat/completions": true,
}

// isConversationEndpoint 判断路径是否为会话型推理端点。
func isConversationEndpoint(path string) bool {
	return conversationEndpoints[path]
}

// rawRequestOf 摘出原始层数据:请求行、脱敏后的入向头与请求体原文。
// 头在入队前统一脱敏,后续管道只接触脱敏副本(design.md D2)。
func rawRequestOf(ctx *chain.Context) rawRequest {
	if ctx.R == nil {
		return rawRequest{}
	}
	headers := ctx.R.Header.Clone()
	// Go 的 http.Header 不含 Host,单独补入以便回溯请求目标。
	if ctx.R.Host != "" {
		headers.Set("Host", ctx.R.Host)
	}
	return rawRequest{
		method:  ctx.R.Method,
		path:    ctx.R.URL.Path,
		query:   trace.SanitizeQuery(ctx.R.URL.RawQuery),
		headers: trace.SanitizeHeaders(headers),
		body:    ctx.Body,
	}
}

// buildRequestBody 把原始数据整理成落库形态:请求体原文 gzip 压缩(design.md D1)。
func buildRequestBody(requestID int64, raw rawRequest) (db.InsertRequestBodyParams, error) {
	compressed, err := trace.Compress(raw.body)
	if err != nil {
		// COVERAGE-EXEMPT: trace.Compress 只写 bytes.Buffer,错误分支不可达。
		return db.InsertRequestBodyParams{}, err
	}
	return db.InsertRequestBodyParams{
		RequestID: requestID,
		Method:    raw.method,
		Path:      raw.path,
		Query:     raw.query,
		BodyGzip:  compressed,
		Headers:   raw.headers,
	}, nil
}
