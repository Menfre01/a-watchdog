package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"ds-context/pkg/chain"
	"ds-context/pkg/config"
	"ds-context/pkg/config/db"
	"ds-context/pkg/protocol"
)

// Recorder 是响应后处理环节:无论请求是否被短路,都异步落一条请求记录。
// finalizer 语义使其能观察被拒绝的请求(对应 request-logging spec)。
type Recorder struct {
	store   InsertStore
	queue   chan db.InsertRequestParams
	logger  *slog.Logger
	dropped atomic.Int64
}

// InsertStore 是记录环节所需的最小写入依赖(config.Store 满足)。
type InsertStore interface {
	InsertRequests(ctx context.Context, reqs []db.InsertRequestParams) error
}

const (
	// recordQueueSize 是有界队列容量:队满时丢弃并计数告警,永不阻塞请求。
	recordQueueSize = 1024
	// recordBatchMax 是写协程单批合并的上限。
	recordBatchMax = 64
	// drainTimeout 是关停窗口内清空剩余记录的时间上限。
	drainTimeout = 2 * time.Second
)

// NewRecorder 创建记录环节并启动后台写协程;ctx 取消时协程退出。
func NewRecorder(ctx context.Context, store InsertStore, logger *slog.Logger) *Recorder {
	r := &Recorder{
		store:  store,
		queue:  make(chan db.InsertRequestParams, recordQueueSize),
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
	params := snapshotRecord(ctx, err)
	select {
	case r.queue <- params:
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
			batch := []db.InsertRequestParams{first}
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

// insert 写入一批记录;失败只落日志,绝不影响请求本身(对应 request-logging spec)。
func (r *Recorder) insert(ctx context.Context, batch []db.InsertRequestParams) {
	if err := r.store.InsertRequests(ctx, batch); err != nil {
		r.logger.Error("请求记录写入失败", "err", err)
	}
}

// drain 在关停窗口内尽力清空队列。
func (r *Recorder) drain() {
	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	var batch []db.InsertRequestParams
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

// snapshotRecord 把请求上下文与结果归集成一条记录参数。
func snapshotRecord(ctx *chain.Context, chainErr error) db.InsertRequestParams {
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
	return params
}
