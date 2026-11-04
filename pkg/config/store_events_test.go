package config

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"

	"awatchdog/pkg/config/db"
)

// mustCreateClientKey 建一个测试用客户端密钥。
func mustCreateClientKey(t *testing.T, s *Store, name, hash string) db.ClientKey {
	t.Helper()
	k, err := s.CreateClientKey(context.Background(), db.CreateClientKeyParams{
		Name: name, KeyHash: hash, KeyPrefix: "awd_" + name, Enabled: 1,
	})
	if err != nil {
		t.Fatalf("CreateClientKey(%s) error = %v", name, err)
	}
	return k
}

// TestStore_DeleteClientKey 覆盖删除成功、快照即时失效与重复删除返回 ErrNotFound。
func TestStore_DeleteClientKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	k := mustCreateClientKey(t, s, "claude-code", "sha256:del")
	keep := mustCreateClientKey(t, s, "codex", "sha256:keep")

	if err := s.DeleteClientKey(ctx, k.ID); err != nil {
		t.Fatalf("DeleteClientKey() error = %v", err)
	}
	// 快照即时生效:被删密钥不再命中,其他密钥不受影响。
	if _, ok := s.Snapshot().LookupKey("sha256:del"); ok {
		t.Fatal("被删密钥仍在快照中")
	}
	if info, ok := s.Snapshot().LookupKey("sha256:keep"); !ok || info.ID != keep.ID {
		t.Fatalf("误删其他密钥: %+v, %v", info, ok)
	}
	keys, err := s.ListClientKeys(ctx)
	if err != nil || len(keys) != 1 || keys[0].ID != keep.ID {
		t.Fatalf("ListClientKeys() = %+v, %v; want 仅保留 %d", keys, err, keep.ID)
	}
	// 重复删除:影响 0 行,必须显式报 ErrNotFound 而不是静默成功。
	if err := s.DeleteClientKey(ctx, k.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteClientKey(missing) error = %v, want ErrNotFound", err)
	}
}

// TestStore_ResponseBody 覆盖响应原文的写入、读回、幂等与约束错误。
func TestStore_ResponseBody(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()
	reqID := insertTraceRequest(t, s, "2026-10-13T09:00:00Z", 1)

	first := db.InsertResponseBodyParams{
		RequestID: reqID,
		BodyGzip:  []byte{0x1f, 0x8b, 0x01},
		Truncated: 1,
		Complete:  0,
	}
	if err := s.InsertResponseBody(ctx, first); err != nil {
		t.Fatalf("InsertResponseBody() error = %v", err)
	}
	got, err := s.ResponseBody(ctx, reqID)
	if err != nil {
		t.Fatalf("ResponseBody() error = %v", err)
	}
	if got.RequestID != reqID || !bytes.Equal(got.BodyGzip, first.BodyGzip) ||
		got.Truncated != 1 || got.Complete != 0 {
		t.Fatalf("响应原文 = %+v, want %+v", got, first)
	}

	// 重复写入:ON CONFLICT DO NOTHING,保留首条,不报错。
	second := db.InsertResponseBodyParams{RequestID: reqID, BodyGzip: []byte{0x00}, Complete: 1}
	if err := s.InsertResponseBody(ctx, second); err != nil {
		t.Fatalf("InsertResponseBody(duplicate) error = %v", err)
	}
	got, err = s.ResponseBody(ctx, reqID)
	if err != nil {
		t.Fatalf("ResponseBody(after duplicate) error = %v", err)
	}
	if !bytes.Equal(got.BodyGzip, first.BodyGzip) || got.Complete != 0 {
		t.Fatalf("重复写入覆盖了首条: %+v", got)
	}

	// 外键约束:请求记录不存在时写入失败。
	if err := s.InsertResponseBody(ctx, db.InsertResponseBodyParams{
		RequestID: 9999, BodyGzip: []byte{0x00},
	}); err == nil {
		t.Fatal("InsertResponseBody(unknown request) error = nil, want 外键约束失败")
	}
	// 无记录:返回底层 sql.ErrNoRows。
	if _, err := s.ResponseBody(ctx, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ResponseBody(missing) error = %v, want sql.ErrNoRows", err)
	}
}

// TestStore_AggregateRequestsSince 覆盖时间窗口内的请求数、错误数与用量汇总,
// 以及延迟序列的升序返回。
func TestStore_AggregateRequestsSince(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()

	old := int64(10)
	midIn, midOut, midCache := int64(5), int64(3), int64(2)
	newIn := int64(1)
	reqs := []db.InsertRequestParams{
		{Ts: "2026-10-12T00:00:00Z", Protocol: "anthropic-messages", StatusCode: 200, LatencyMs: 99, UsageIn: &old},
		{Ts: "2026-10-13T09:00:00Z", Protocol: "anthropic-messages", StatusCode: 200, LatencyMs: 30,
			UsageIn: &midIn, UsageOut: &midOut, UsageCache: &midCache},
		{Ts: "2026-10-13T09:01:00Z", Protocol: "openai-chat", StatusCode: 502, LatencyMs: 10, UsageIn: &newIn},
	}
	// error_kind 非空也计为错误(与查询里的判定一致)。
	reqs[2].ErrorKind = strPtr("upstream")
	if _, err := s.InsertRequests(ctx, reqs); err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}

	row, err := s.AggregateRequestsSince(ctx, "2026-10-13T00:00:00Z")
	if err != nil {
		t.Fatalf("AggregateRequestsSince() error = %v", err)
	}
	if row.Count != 2 || row.Errors != 1 {
		t.Errorf("窗口内 count/errors = %d/%d, want 2/1", row.Count, row.Errors)
	}
	if row.UsageIn != 6 || row.UsageOut != 3 || row.UsageCache != 2 {
		t.Errorf("窗口内用量 = %d/%d/%d, want 6/3/2", row.UsageIn, row.UsageOut, row.UsageCache)
	}

	// 空窗口:聚合值为 0,不报错。
	row, err = s.AggregateRequestsSince(ctx, "2026-10-14T00:00:00Z")
	if err != nil {
		t.Fatalf("AggregateRequestsSince(empty) error = %v", err)
	}
	if row.Count != 0 || row.Errors != 0 || row.UsageIn != 0 || row.UsageOut != 0 || row.UsageCache != 0 {
		t.Fatalf("空窗口聚合 = %+v, want 全 0", row)
	}

	// 延迟序列:仅窗口内,且升序(分位数计算依赖有序输入)。
	lat, err := s.ListRequestLatenciesSince(ctx, "2026-10-13T00:00:00Z")
	if err != nil {
		t.Fatalf("ListRequestLatenciesSince() error = %v", err)
	}
	if len(lat) != 2 || lat[0] != 10 || lat[1] != 30 {
		t.Fatalf("延迟序列 = %v, want [10 30]", lat)
	}
	lat, err = s.ListRequestLatenciesSince(ctx, "2026-10-14T00:00:00Z")
	if err != nil || len(lat) != 0 {
		t.Fatalf("空窗口延迟 = %v, %v; want 空", lat, err)
	}
}

// strPtr 返回 string 的指针,供可空列参数使用。
func strPtr(v string) *string { return &v }

// TestStore_ToolEvents 覆盖工具事件的写入、幂等、会话隔离与读取排序。
func TestStore_ToolEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	event := func(session, callID, phase, ts string, wait int64) db.InsertToolEventParams {
		return db.InsertToolEventParams{
			Client:          "claude-code",
			SessionID:       session,
			TurnID:          "turn-1",
			ToolCallID:      callID,
			ToolName:        "Bash",
			Phase:           phase,
			Status:          "ok",
			Boundary:        "hook",
			ApprovalWait:    wait,
			Ts:              ts,
			InputGzip:       []byte{0x1f, 0x8b, 0x02},
			InputTruncated:  1,
			ResultGzip:      []byte{0x1f, 0x8b, 0x03},
			ResultTruncated: 1,
		}
	}

	if err := s.InsertToolEvent(ctx, event("1:sess", "call-1", "start", "2026-10-13T09:00:00Z", 0)); err != nil {
		t.Fatalf("InsertToolEvent(start) error = %v", err)
	}
	if err := s.InsertToolEvent(ctx, event("1:sess", "call-1", "end", "2026-10-13T09:00:05Z", 1200)); err != nil {
		t.Fatalf("InsertToolEvent(end) error = %v", err)
	}
	// 幂等:(client, tool_call_id, phase) 重复上报被忽略(时间戳不得覆盖原值)。
	if err := s.InsertToolEvent(ctx, event("1:sess", "call-1", "start", "2026-10-13T10:00:00Z", 0)); err != nil {
		t.Fatalf("InsertToolEvent(duplicate) error = %v", err)
	}
	// 会话隔离:其他会话的事件不混入。
	if err := s.InsertToolEvent(ctx, event("2:other", "call-2", "start", "2026-10-13T08:00:00Z", 0)); err != nil {
		t.Fatalf("InsertToolEvent(other session) error = %v", err)
	}

	got, err := s.ListToolEvents(ctx, "1:sess")
	if err != nil {
		t.Fatalf("ListToolEvents() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("事件数 = %d, want 2(%+v)", len(got), got)
	}
	// 按 ts、id 升序:start 在前。
	if got[0].Phase != "start" || got[0].Ts != "2026-10-13T09:00:00Z" {
		t.Errorf("首条 = %+v, want start 且时间未被覆盖", got[0])
	}
	if got[1].Phase != "end" || got[1].ApprovalWait != 1200 {
		t.Errorf("次条 = %+v, want end 且审批等待 1200", got[1])
	}
	// 载荷字段完整落库。
	if !bytes.Equal(got[0].InputGzip, []byte{0x1f, 0x8b, 0x02}) ||
		!bytes.Equal(got[0].ResultGzip, []byte{0x1f, 0x8b, 0x03}) ||
		got[0].InputTruncated != 1 || got[0].ResultTruncated != 1 {
		t.Errorf("载荷字段 = %+v", got[0])
	}

	// 未知会话返回空而非报错。
	if empty, err := s.ListToolEvents(ctx, "9:none"); err != nil || len(empty) != 0 {
		t.Fatalf("未知会话 = %v, %v; want 空", empty, err)
	}
	// phase 受 CHECK 约束:非法取值必须被拒。
	if err := s.InsertToolEvent(ctx, event("1:sess", "call-3", "middle", "2026-10-13T09:01:00Z", 0)); err == nil {
		t.Fatal("InsertToolEvent(phase=middle) error = nil, want CHECK 约束失败")
	}
}

// TestStore_PromptEvents 覆盖提示提交事件的写入、去重规则与读取排序。
func TestStore_PromptEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	submit := func(session, promptID, ts string, length int64) db.InsertPromptEventParams {
		return db.InsertPromptEventParams{
			Client:    "claude-code",
			SessionID: session,
			TurnID:    "turn-1",
			PromptID:  promptID,
			PromptLen: length,
			Ts:        ts,
		}
	}

	if err := s.InsertPromptEvent(ctx, submit("1:sess", "p1", "2026-10-13T09:00:00Z", 100)); err != nil {
		t.Fatalf("InsertPromptEvent() error = %v", err)
	}
	// 同一 prompt_id 重复上报被忽略(长度保留首次上报值)。
	if err := s.InsertPromptEvent(ctx, submit("1:sess", "p1", "2026-10-13T09:30:00Z", 999)); err != nil {
		t.Fatalf("InsertPromptEvent(duplicate) error = %v", err)
	}
	// 无 prompt_id 的提交逐条入库:部分唯一索引只覆盖非空 prompt_id。
	if err := s.InsertPromptEvent(ctx, submit("1:sess", "", "2026-10-13T09:01:00Z", 10)); err != nil {
		t.Fatalf("InsertPromptEvent(empty id, first) error = %v", err)
	}
	if err := s.InsertPromptEvent(ctx, submit("1:sess", "", "2026-10-13T09:02:00Z", 20)); err != nil {
		t.Fatalf("InsertPromptEvent(empty id, second) error = %v", err)
	}
	// 会话隔离。
	if err := s.InsertPromptEvent(ctx, submit("2:other", "p2", "2026-10-13T08:00:00Z", 7)); err != nil {
		t.Fatalf("InsertPromptEvent(other session) error = %v", err)
	}

	got, err := s.ListPromptEvents(ctx, "1:sess")
	if err != nil {
		t.Fatalf("ListPromptEvents() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("事件数 = %d, want 3(%+v)", len(got), got)
	}
	want := []struct {
		ts     string
		length int64
	}{
		{"2026-10-13T09:00:00Z", 100},
		{"2026-10-13T09:01:00Z", 10},
		{"2026-10-13T09:02:00Z", 20},
	}
	for i, w := range want {
		if got[i].Ts != w.ts || got[i].PromptLen != w.length {
			t.Errorf("第 %d 条 = %+v, want ts=%s len=%d", i, got[i], w.ts, w.length)
		}
	}
	if got[0].PromptID != "p1" || got[0].Client != "claude-code" || got[0].TurnID != "turn-1" {
		t.Errorf("首条字段 = %+v", got[0])
	}

	// 未知会话返回空而非报错。
	if empty, err := s.ListPromptEvents(ctx, "9:none"); err != nil || len(empty) != 0 {
		t.Fatalf("未知会话 = %v, %v; want 空", empty, err)
	}
}
