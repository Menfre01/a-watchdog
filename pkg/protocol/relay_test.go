package protocol

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// doGet 发起带 context 的 GET(测试辅助,避免 noctx 告警)。
func doGet(t *testing.T, ctx context.Context, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// writeAndFlush 写一块并立即 Flush(模拟上游流式输出)。
func writeAndFlush(t *testing.T, w http.ResponseWriter, s string) {
	t.Helper()
	if _, err := io.WriteString(w, s); err != nil {
		t.Errorf("upstream write: %v", err)
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func TestRelayStream_DoesNotBuffer(t *testing.T) {
	var releaseOnce sync.Once
	release := make(chan struct{})
	releaseNow := func() { releaseOnce.Do(func() { close(release) }) }

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeAndFlush(t, w, "data: first\n\n")
		<-release // 第二块等客户端确认收到第一块之后才放行
		writeAndFlush(t, w, "data: second\n\n")
	}))
	defer upstream.Close()
	defer releaseNow()

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream.URL, nil)
		if err != nil {
			t.Errorf("new upstream request: %v", err)
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("upstream call: %v", err)
			http.Error(w, "upstream failure", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if err := RelayStream(w, resp.Body, &Observer{}); err != nil {
			t.Errorf("relay: %v", err)
		}
	}))
	defer relay.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp := doGet(t, ctx, relay.URL)
	defer func() { _ = resp.Body.Close() }()

	type firstChunk struct {
		line string
		err  error
	}
	ch := make(chan firstChunk, 1)
	br := bufio.NewReader(resp.Body)
	go func() {
		line, err := br.ReadString('\n')
		ch <- firstChunk{line: line, err: err}
	}()

	select {
	case fc := <-ch:
		if fc.err != nil || !strings.Contains(fc.line, "first") {
			releaseNow()
			t.Fatalf("first chunk = %q, %v", fc.line, fc.err)
		}
	case <-time.After(5 * time.Second):
		releaseNow()
		t.Fatal("第一块数据未在上游放行前到达:转发疑似整体缓冲")
	}
	releaseNow()

	rest, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("read rest: %v", err)
	}
	if !strings.Contains(string(rest), "second") {
		t.Fatalf("rest = %q, want second chunk", rest)
	}
}

func TestRelayStream_CancelsUpstreamOnClientDisconnect(t *testing.T) {
	sawCancel := make(chan struct{}, 1)
	timedOut := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeAndFlush(t, w, "data: open\n\n")
		select {
		case <-r.Context().Done():
			sawCancel <- struct{}{}
		case <-time.After(8 * time.Second):
			timedOut <- struct{}{}
		}
	}))
	defer upstream.Close()

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream.URL, nil)
		if err != nil {
			t.Errorf("new upstream request: %v", err)
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("upstream call: %v", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		// 客户端断开后转发必然出错,此处属预期。
		_ = RelayStream(w, resp.Body, &Observer{})
	}))
	defer relay.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp := doGet(t, ctx, relay.URL)

	br := bufio.NewReader(resp.Body)
	if line, err := br.ReadString('\n'); err != nil || !strings.Contains(line, "open") {
		t.Fatalf("first line = %q, %v", line, err)
	}
	// 模拟客户端断开。
	cancel()
	_ = resp.Body.Close()

	select {
	case <-sawCancel:
		// 取消传播成功。
	case <-timedOut:
		t.Fatal("客户端断开后 8 秒内上游未收到取消")
	case <-time.After(10 * time.Second):
		t.Fatal("等待取消传播超时")
	}
}

func TestObserver_AnthropicSSEUsage(t *testing.T) {
	stream := "event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"m1","usage":{"input_tokens":100,"output_tokens":1,"cache_read_input_tokens":40,"cache_creation_input_tokens":20}}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":55}}` + "\n\n"

	obs := NewObserver("anthropic-messages", "text/event-stream")
	// 以不规则分块喂入(7 字节一步,跨行切断),验证增量解析健壮性。
	for i := 0; i < len(stream); i += 7 {
		obs.Write([]byte(stream[i:min(i+7, len(stream))]))
	}
	u := obs.Usage()
	if u.In == nil || *u.In != 100 {
		t.Fatalf("In = %v, want 100", u.In)
	}
	if u.Out == nil || *u.Out != 55 {
		t.Fatalf("Out = %v, want 55", u.Out)
	}
	if u.Cache == nil || *u.Cache != 60 {
		t.Fatalf("Cache = %v, want 60", u.Cache)
	}
}

func TestAnthropicSSEUsage_CacheInMessageDelta(t *testing.T) {
	// 新版本 API 在 message_delta 的 usage 中携带累计 cache 用量,必须同样被提取。
	stream := "event: message_delta\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":55,"cache_read_input_tokens":30,"cache_creation_input_tokens":10}}` + "\n\n"
	obs := NewObserver("anthropic-messages", "text/event-stream")
	obs.Write([]byte(stream))
	u := obs.Usage()
	if u.Out == nil || *u.Out != 55 || u.Cache == nil || *u.Cache != 40 {
		t.Fatalf("usage = %+v, want out=55 cache=40", u)
	}
}

func TestObserver_OpenAISSEUsage(t *testing.T) {
	stream := `data: {"id":"c1","usage":null}` + "\n\n" +
		`data: {"id":"c1","usage":{"prompt_tokens":12,"completion_tokens":34,"prompt_tokens_details":{"cached_tokens":8}}}` + "\n\n" +
		"data: [DONE]\n\n"
	obs := NewObserver("openai-chat", "text/event-stream")
	obs.Write([]byte(stream))
	u := obs.Usage()
	if u.In == nil || *u.In != 12 || u.Out == nil || *u.Out != 34 || u.Cache == nil || *u.Cache != 8 {
		t.Fatalf("usage = %+v", u)
	}
}

func TestObserver_JSONUsage(t *testing.T) {
	oa := NewObserver("openai-chat", "application/json; charset=utf-8")
	oa.Write([]byte(`{"usage":{"prompt_tokens":3,"completion_tokens":4}}`))
	if u := oa.Usage(); u.In == nil || *u.In != 3 || u.Out == nil || *u.Out != 4 {
		t.Fatalf("openai usage = %+v", u)
	}

	an := NewObserver("anthropic-messages", "application/json")
	an.Write([]byte(`{"usage":{"input_tokens":7,"output_tokens":9,"cache_read_input_tokens":1,"cache_creation_input_tokens":2}}`))
	if u := an.Usage(); u.In == nil || *u.In != 7 || u.Out == nil || *u.Out != 9 || u.Cache == nil || *u.Cache != 3 {
		t.Fatalf("anthropic usage = %+v", u)
	}
}

func TestObserver_TolerantOfGarbage(t *testing.T) {
	obs := NewObserver("anthropic-messages", "text/event-stream")
	obs.Write([]byte("not sse at all\n"))
	obs.Write([]byte("data: {broken json\n\n"))
	if u := obs.Usage(); u.In != nil || u.Out != nil || u.Cache != nil {
		t.Fatalf("garbage usage = %+v, want empty", u)
	}

	noop := NewObserver("unknown-protocol", "application/octet-stream")
	noop.Write([]byte("x"))
	if u := noop.Usage(); u.In != nil || u.Out != nil || u.Cache != nil {
		t.Fatalf("noop usage = %+v, want empty", u)
	}
}
