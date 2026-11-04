package config

import (
	"context"
	"path/filepath"
	"testing"

	"awatchdog/pkg/config/db"
)

// openTraceStore 打开一个临时库并注册清理。
func openTraceStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "trace.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return s
}

// insertTraceRequest 写入一条最小请求记录并返回其 id(测试夹具)。
func insertTraceRequest(t *testing.T, s *Store, ts string, usageIn int64) int64 {
	t.Helper()
	usage := usageIn
	ids, err := s.InsertRequests(context.Background(), []db.InsertRequestParams{{
		Ts:         ts,
		Protocol:   "anthropic-messages",
		StatusCode: 200,
		LatencyMs:  10,
		UsageIn:    &usage,
	}})
	if err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("InsertRequests() 返回 %d 个 id, want 1", len(ids))
	}
	return ids[0]
}

// insertTraceSnapshot 写入一条快照(测试夹具)。
func insertTraceSnapshot(t *testing.T, s *Store, reqID int64, sessionID string, seq int64, ts string) {
	t.Helper()
	err := s.InsertSnapshot(context.Background(), db.InsertSnapshotParams{
		RequestID:     reqID,
		SessionID:     sessionID,
		SessionSource: "explicit",
		SessionNote:   "explicit: x-claude-code-session-id",
		Seq:           seq,
		Ts:            ts,
		NdjsonGzip:    []byte{0x1f, 0x8b, 0x08},
		LineHashes:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
		LineSizes:     []byte{0, 0, 0, 5},
	})
	if err != nil {
		t.Fatalf("InsertSnapshot(seq=%d) error = %v", seq, err)
	}
}

// TestStore_InsertRequestsReturnsMatchingIDs 断言逐条 RETURNING id 的结果
// 与入参一一对应:id 递增,且按 id 回查的顺序与写入顺序一致(design.md D2)。
func TestStore_InsertRequestsReturnsMatchingIDs(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()

	reqs := []db.InsertRequestParams{
		{Ts: "2026-10-13T09:00:00Z", Protocol: "anthropic-messages", RequestedModel: "first", StatusCode: 200},
		{Ts: "2026-10-13T09:01:00Z", Protocol: "openai-chat", RequestedModel: "second", StatusCode: 200},
		{Ts: "2026-10-13T09:02:00Z", Protocol: "openai-chat", RequestedModel: "third", StatusCode: 500},
	}
	ids, err := s.InsertRequests(ctx, reqs)
	if err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}
	if len(ids) != len(reqs) {
		t.Fatalf("id 数 = %d, want %d", len(ids), len(reqs))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("id 未严格递增: %v", ids)
		}
	}

	// ListRecentRequests 按 id 倒序:倒序回读的模型名应与入参顺序相反。
	rows, err := s.ListRecentRequests(ctx, int64(len(reqs)))
	if err != nil {
		t.Fatalf("ListRecentRequests() error = %v", err)
	}
	if len(rows) != len(reqs) {
		t.Fatalf("回查行数 = %d, want %d", len(rows), len(reqs))
	}
	want := []string{"third", "second", "first"}
	for i, w := range want {
		if rows[i].RequestedModel != w {
			t.Errorf("倒序第 %d 行 = %q, want %q", i, rows[i].RequestedModel, w)
		}
		if rows[i].ID != ids[len(ids)-1-i] {
			t.Errorf("倒序第 %d 行 id = %d, want %d", i, rows[i].ID, ids[len(ids)-1-i])
		}
	}
}

// TestStore_InsertRequestsEmpty 覆盖空入参:不开启事务,返回 nil。
func TestStore_InsertRequestsEmpty(t *testing.T) {
	s := openTraceStore(t)
	ids, err := s.InsertRequests(context.Background(), nil)
	if err != nil || ids != nil {
		t.Fatalf("InsertRequests(nil) = %v, %v; want nil, nil", ids, err)
	}
}

// TestStore_TraceRoundTrip 覆盖原始层与事实层的写入与读回。
func TestStore_TraceRoundTrip(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()
	reqID := insertTraceRequest(t, s, "2026-10-13T09:00:00Z", 100)

	body := db.InsertRequestBodyParams{
		RequestID: reqID,
		Method:    "POST",
		Path:      "/v1/messages",
		Query:     "beta=true",
		BodyGzip:  []byte{0x1f, 0x8b, 0x00},
		Headers:   `{"x-claude-code-session-id":["abc"]}`,
	}
	if err := s.InsertRequestBody(ctx, body); err != nil {
		t.Fatalf("InsertRequestBody() error = %v", err)
	}
	gotBody, err := s.q.GetRequestBodyByRequestID(ctx, reqID)
	if err != nil {
		t.Fatalf("GetRequestBodyByRequestID() error = %v", err)
	}
	if gotBody.Path != "/v1/messages" || gotBody.Query != "beta=true" || gotBody.Method != "POST" {
		t.Errorf("原始层请求行 = %+v", gotBody)
	}
	if string(gotBody.BodyGzip) != string(body.BodyGzip) || gotBody.Headers != body.Headers {
		t.Errorf("原始层 body/headers 不一致: %+v", gotBody)
	}

	insertTraceSnapshot(t, s, reqID, "1:abc", 1, "2026-10-13T09:00:00Z")

	snaps, err := s.ListSnapshotsBySession(ctx, "1:abc")
	if err != nil {
		t.Fatalf("ListSnapshotsBySession() error = %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("快照数 = %d, want 1", len(snaps))
	}
	got := snaps[0]
	if got.RequestID != reqID || got.SessionID != "1:abc" || got.Seq != 1 {
		t.Errorf("快照主字段 = %+v", got)
	}
	if got.SessionSource != "explicit" || got.SessionNote == "" {
		t.Errorf("判定来源/理由未落盘: %+v", got)
	}
	if len(got.LineHashes) != 8 || len(got.LineSizes) != 4 {
		t.Errorf("派生列长度 = %d/%d, want 8/4", len(got.LineHashes), len(got.LineSizes))
	}

	// NextSnapshotSeq 在同一会话内递增，且跨会话独立。
	seq, err := s.NextSnapshotSeq(ctx, "1:abc")
	if err != nil || seq != 2 {
		t.Fatalf("NextSnapshotSeq(1:abc) = %d, %v; want 2, nil", seq, err)
	}
	seq, err = s.NextSnapshotSeq(ctx, "1:other")
	if err != nil || seq != 1 {
		t.Fatalf("NextSnapshotSeq(1:other) = %d, %v; want 1, nil", seq, err)
	}
}

// TestStore_ListSessionSummaries 覆盖会话列表聚合与过滤。
func TestStore_ListSessionSummaries(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()

	// 会话 A:两个 step(usage 累加);会话 B:一个 step。A 更晚活跃。
	a1 := insertTraceRequest(t, s, "2026-10-13T09:00:00Z", 100)
	a2 := insertTraceRequest(t, s, "2026-10-13T09:30:00Z", 200)
	b1 := insertTraceRequest(t, s, "2026-10-12T10:00:00Z", 7)
	insertTraceSnapshot(t, s, a1, "1:sessA", 1, "2026-10-13T09:00:00Z")
	insertTraceSnapshot(t, s, a2, "1:sessA", 2, "2026-10-13T09:30:00Z")
	insertTraceSnapshot(t, s, b1, "2:sessB", 1, "2026-10-12T10:00:00Z")

	got, err := s.ListSessionSummaries(ctx, "", "", "", "", 10)
	if err != nil {
		t.Fatalf("ListSessionSummaries() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("会话数 = %d, want 2(%+v)", len(got), got)
	}
	// 按 last_seen 倒序:会话 A 在前。
	if got[0].SessionID != "1:sessA" {
		t.Fatalf("首行 = %q, want 1:sessA", got[0].SessionID)
	}
	if got[0].Steps != 2 || got[0].UsageIn != 300 {
		t.Errorf("会话 A 聚合 = steps %d / usage_in %d, want 2 / 300", got[0].Steps, got[0].UsageIn)
	}
	if got[0].FirstSeen != "2026-10-13T09:00:00Z" || got[0].LastSeen != "2026-10-13T09:30:00Z" {
		t.Errorf("会话 A 首末 = %s → %s", got[0].FirstSeen, got[0].LastSeen)
	}
	if got[0].SessionSource != "explicit" || got[0].SessionNote == "" {
		t.Errorf("会话 A 判定信息缺失: %+v", got[0])
	}
	if got[1].SessionID != "2:sessB" || got[1].Steps != 1 || got[1].UsageIn != 7 {
		t.Errorf("会话 B 聚合 = %+v", got[1])
	}

	// 密钥过滤:只取 client key 2 的会话。
	got, err = s.ListSessionSummaries(ctx, "2", "", "", "", 10)
	if err != nil || len(got) != 1 || got[0].SessionID != "2:sessB" {
		t.Fatalf("密钥过滤 = %+v, %v", got, err)
	}

	// 原始标识前缀过滤:匹配冒号之后的部分,与界面显示一致。
	got, err = s.ListSessionSummaries(ctx, "", "sessB", "", "", 10)
	if err != nil || len(got) != 1 || got[0].SessionID != "2:sessB" {
		t.Fatalf("标识前缀过滤 = %+v, %v", got, err)
	}

	// 两个条件叠加。
	got, err = s.ListSessionSummaries(ctx, "1", "sessA", "", "", 10)
	if err != nil || len(got) != 1 || got[0].SessionID != "1:sessA" {
		t.Fatalf("叠加过滤 = %+v, %v", got, err)
	}

	// 时间过滤:会话内有任一步落在区间内即入选(A 在区间内,B 不在)。
	got, err = s.ListSessionSummaries(ctx, "", "", "2026-10-13T00:00:00Z", "2026-10-13T23:59:59Z", 10)
	if err != nil || len(got) != 1 || got[0].SessionID != "1:sessA" {
		t.Fatalf("时间过滤 = %+v, %v", got, err)
	}

	// 行数上限。
	got, err = s.ListSessionSummaries(ctx, "", "", "", "", 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("row_limit = %+v, %v", got, err)
	}
}

// TestStore_ListSessionSteps 覆盖会话轨迹组装查询:快照派生列与请求记录字段一次取齐。
func TestStore_ListSessionSteps(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()
	reqID := insertTraceRequest(t, s, "2026-10-13T09:00:00Z", 100)
	insertTraceSnapshot(t, s, reqID, "1:sess", 1, "2026-10-13T09:00:00Z")

	steps, err := s.ListSessionSteps(ctx, "1:sess")
	if err != nil {
		t.Fatalf("ListSessionSteps() error = %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("步数 = %d, want 1", len(steps))
	}
	got := steps[0]
	if got.RequestID != reqID || got.Seq != 1 || got.SessionID != "1:sess" {
		t.Errorf("组装结果 = %+v", got)
	}
	// 请求记录字段同批取回(骨架需要延迟、状态与用量)。
	if got.StatusCode != 200 || got.LatencyMs != 10 {
		t.Errorf("请求字段 = %+v", got)
	}
	if got.UsageIn == nil || *got.UsageIn != 100 {
		t.Errorf("用量字段 = %+v", got.UsageIn)
	}
	// 派生列齐备(比对只读它们)。
	if len(got.LineHashes) != 8 || len(got.LineSizes) != 4 {
		t.Errorf("派生列长度 = %d/%d, want 8/4", len(got.LineHashes), len(got.LineSizes))
	}
	// 未知会话返回空而非报错。
	if empty, err := s.ListSessionSteps(ctx, "9:none"); err != nil || len(empty) != 0 {
		t.Errorf("未知会话 = %v, %v", empty, err)
	}
}
