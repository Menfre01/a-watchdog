// Package server 装配网关的 HTTP 入口:端点注册、请求上下文构造与错误形状写出
// (设计见 design.md D1/D3;行为契约见 specs/protocol-routing)。
package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"awatchdog/pkg/chain"
	"awatchdog/pkg/config"
	"awatchdog/pkg/protocol"
)

// maxBodyBytes 是入向请求体的体积上限(长上下文对话可达数 MB)。
const maxBodyBytes = 64 << 20

// Server 是网关的 HTTP 入口。
type Server struct {
	handler chain.Handler
	logger  *slog.Logger
	console http.Handler
	tools   *toolEvents
	// maxBody 是入向请求体的体积上限;New 使用 maxBodyBytes,测试可覆盖以走限流分支。
	maxBody int64
}

// New 创建入口:chainHandler 是装配完成的整条责任链(终结点为上游调用)。
func New(chainHandler chain.Handler, logger *slog.Logger) *Server {
	return &Server{handler: chainHandler, logger: logger, maxBody: maxBodyBytes}
}

// WithToolEvents 挂载工具事件上报端点(路径 /v1/tool-events);未挂载时该路径返回 404。
func (s *Server) WithToolEvents(store *config.Store) *Server {
	s.tools = &toolEvents{store: store, snapshot: store.Snapshot}
	return s
}

// WithConsole 挂载控制台(路径 /console);未挂载时该路径返回 404。
func (s *Server) WithConsole(h http.Handler) *Server {
	s.console = h
	return s
}

// Handler 返回注册好端点的 http.Handler。
// 路径按调研结论匹配(忽略查询串),覆盖 /v1/messages 与 count_tokens 端点。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.endpoint("openai-chat"))
	mux.HandleFunc("POST /v1/messages/count_tokens", s.endpoint("anthropic-messages"))
	mux.HandleFunc("POST /v1/messages", s.endpoint("anthropic-messages"))
	if s.tools != nil {
		mux.Handle("POST /v1/tool-events", s.tools)
		mux.HandleFunc("POST /v1/prompt-events", s.tools.handlePrompt)
	}
	if s.console != nil {
		mux.Handle("/console/", s.console)
		mux.Handle("/console", s.console)
	}
	return mux
}

// endpoint 返回某协议的入向处理器:读取请求体、构造上下文、执行责任链,
// 并按入向协议形状写出错误(受控拒绝原样写出;内部错误在响应未开始时写 502)。
func (s *Server) endpoint(protocolName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.maxBody))
		if err != nil {
			s.writeRejected(w, protocolName, http.StatusBadRequest, "invalid_request", "request body too large or unreadable")
			return
		}
		ctx := &chain.Context{
			W:         w,
			R:         r,
			Protocol:  protocolName,
			Body:      body,
			StartedAt: time.Now(),
		}
		// 单请求 panic 隔离:链上任一环节 panic 时不中断进程;响应未开始时补写 502。
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("请求处理发生 panic", "protocol", protocolName, "panic", fmt.Sprint(rec))
				if !ctx.ResponseStarted {
					s.writeRejected(w, protocolName, http.StatusBadGateway, "internal_error", "internal error")
				}
			}
		}()
		err = s.handler(ctx)
		if err == nil {
			return
		}
		var rejected *chain.RejectError
		if errors.As(err, &rejected) {
			if ctx.ResponseStarted {
				s.logger.Error("响应已开始,无法写出拒绝响应", "err", err)
				return
			}
			w.Header().Set("Content-Type", rejected.ContentType)
			w.WriteHeader(rejected.StatusCode)
			_, _ = w.Write(rejected.Body)
			return
		}
		if r.Context().Err() != nil {
			// 客户端主动断开:属正常客户端行为,降级为 Info。
			s.logger.Info("客户端连接中断", "protocol", protocolName)
			return
		}
		// 内部 / 上游错误:响应未开始时按协议形状写 502;已开始(流中断)则只能记日志。
		s.logger.Error("请求处理失败", "protocol", protocolName, "err", err)
		if ctx.ResponseStarted {
			return
		}
		s.writeRejected(w, protocolName, http.StatusBadGateway, "upstream_error", "upstream request failed")
	}
}

// writeRejected 按入向协议形状写出网关错误响应。
func (s *Server) writeRejected(w http.ResponseWriter, protocolName string, status int, kind, message string) {
	contentType, body := "application/json", []byte(`{"error":"internal error"}`)
	if a, err := protocol.Get(protocolName); err == nil {
		contentType, body = a.ErrorResponse(status, kind, message)
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
