package config

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"

	"ds-context/pkg/config/db"
)

// traceFixture 返回一组最小可用的原始层参数与快照参数(测试夹具)。
func traceFixture(requestID int64) (db.InsertRequestBodyParams, *TraceSnapshot) {
	return db.InsertRequestBodyParams{
			RequestID: requestID,
			Method:    "POST",
			Path:      "/v1/messages",
			Query:     "beta=true",
			BodyGzip:  []byte{0x1f, 0x8b, 0x01},
			Headers:   `{"x-claude-code-session-id":["abc"]}`,
		}, &TraceSnapshot{
			SessionID:     "1:abc",
			SessionSource: "explicit",
			SessionNote:   "explicit: x-claude-code-session-id",
			Seq:           1,
			Ts:            "2026-10-13T09:00:00Z",
			NdjsonGzip:    []byte{0x1f, 0x8b, 0x02},
			LineHashes:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
			LineSizes:     []byte{0, 0, 0, 9},
		}
}

// TestStore_SaveTraceWritesBothLayers 覆盖正常路径:原始层与事实层同事务写入且字段完整。
func TestStore_SaveTraceWritesBothLayers(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()
	reqID := insertTraceRequest(t, s, "2026-10-13T09:00:00Z", 100)
	body, snap := traceFixture(reqID)

	if err := s.SaveTrace(ctx, body, snap); err != nil {
		t.Fatalf("SaveTrace() error = %v", err)
	}

	gotBody, err := s.q.GetRequestBodyByRequestID(ctx, reqID)
	if err != nil {
		t.Fatalf("原始层读回失败: %v", err)
	}
	if gotBody.Method != body.Method || gotBody.Path != body.Path ||
		gotBody.Query != body.Query || gotBody.Headers != body.Headers {
		t.Errorf("原始层字段 = %+v, want %+v", gotBody, body)
	}
	if !bytes.Equal(gotBody.BodyGzip, body.BodyGzip) || gotBody.RequestID != reqID {
		t.Errorf("原始层二进制或关联键 = %+v", gotBody)
	}

	gotSnap, err := s.q.GetSnapshotByRequestID(ctx, reqID)
	if err != nil {
		t.Fatalf("事实层读回失败: %v", err)
	}
	if gotSnap.SessionID != snap.SessionID || gotSnap.Seq != snap.Seq ||
		gotSnap.SessionSource != snap.SessionSource || gotSnap.SessionNote != snap.SessionNote ||
		gotSnap.Ts != snap.Ts || gotSnap.RequestID != reqID {
		t.Errorf("快照字段 = %+v, want %+v", gotSnap, snap)
	}
	if !bytes.Equal(gotSnap.NdjsonGzip, snap.NdjsonGzip) {
		t.Error("快照 ndjson 字节未完整落库")
	}
	if !bytes.Equal(gotSnap.LineHashes, snap.LineHashes) || !bytes.Equal(gotSnap.LineSizes, snap.LineSizes) {
		t.Errorf("派生列 = %v/%v, want %v/%v",
			gotSnap.LineHashes, gotSnap.LineSizes, snap.LineHashes, snap.LineSizes)
	}
}

// TestStore_SaveTraceRollsBackBodyOnSnapshotFailure 覆盖轨迹事务内的原子性:
// 原始层已插入、事实层插入失败时,整个轨迹事务回滚,不留半条轨迹(design.md D2)。
func TestStore_SaveTraceRollsBackBodyOnSnapshotFailure(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()
	reqID := insertTraceRequest(t, s, "2026-10-13T09:00:00Z", 100)

	// 先手工占住 request_id(snapshots.request_id 唯一),让 SaveTrace 第二步必然失败。
	_, fixture := traceFixture(reqID)
	if err := s.InsertSnapshot(ctx, db.InsertSnapshotParams{
		RequestID:     reqID,
		SessionID:     fixture.SessionID,
		SessionSource: fixture.SessionSource,
		SessionNote:   fixture.SessionNote,
		Seq:           fixture.Seq,
		Ts:            fixture.Ts,
		NdjsonGzip:    fixture.NdjsonGzip,
		LineHashes:    fixture.LineHashes,
		LineSizes:     fixture.LineSizes,
	}); err != nil {
		t.Fatalf("占位快照写入失败: %v", err)
	}

	body, snap := traceFixture(reqID)
	if err := s.SaveTrace(ctx, body, snap); err == nil {
		t.Fatal("SaveTrace() error = nil, want 事实层唯一约束失败")
	}

	// 原子性:原始层已插入成功,也必须随事务回滚。
	if _, err := s.q.GetRequestBodyByRequestID(ctx, reqID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("原始层残留半条轨迹, err = %v", err)
	}
	if snaps, err := s.ListSnapshotsBySession(ctx, snap.SessionID); err != nil || len(snaps) != 1 {
		t.Fatalf("快照数 = %d, %v; want 1(仅占位那条)", len(snaps), err)
	}
}

// TestStore_SaveTraceFailureKeepsRecord 覆盖失败隔离:轨迹写入失败时,
// 已提交的请求记录不受影响,且先前写好的轨迹保持完整(design.md D2)。
func TestStore_SaveTraceFailureKeepsRecord(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()

	// 记录事务先独立提交。
	reqID := insertTraceRequest(t, s, "2026-10-13T09:00:00Z", 100)
	body, snap := traceFixture(reqID)
	if err := s.SaveTrace(ctx, body, snap); err != nil {
		t.Fatalf("首次 SaveTrace() error = %v", err)
	}

	// 同一 request_id 再写一次:request_bodies 主键冲突 → 整个轨迹事务回滚。
	if err := s.SaveTrace(ctx, body, snap); err == nil {
		t.Fatal("重复 SaveTrace() error = nil, want 主键冲突")
	}

	// 隔离性:记录仍在。
	if n, err := s.CountRequests(ctx); err != nil || n != 1 {
		t.Fatalf("CountRequests() = %d, %v; want 1", n, err)
	}
	// 先前的轨迹未被破坏。
	snaps, err := s.ListSnapshotsBySession(ctx, "1:abc")
	if err != nil || len(snaps) != 1 {
		t.Fatalf("快照数 = %d, %v; want 1", len(snaps), err)
	}
	if gotBody, err := s.q.GetRequestBodyByRequestID(ctx, reqID); err != nil || gotBody.Path != "/v1/messages" {
		t.Fatalf("原始层被破坏: %+v, %v", gotBody, err)
	}
}

// TestStore_SaveTraceWithoutSnapshot 覆盖请求体不可解析的场景:只落原始层。
func TestStore_SaveTraceWithoutSnapshot(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()
	reqID := insertTraceRequest(t, s, "2026-10-13T09:00:00Z", 100)
	body, _ := traceFixture(reqID)

	if err := s.SaveTrace(ctx, body, nil); err != nil {
		t.Fatalf("SaveTrace(snap=nil) error = %v", err)
	}
	if _, err := s.q.GetRequestBodyByRequestID(ctx, reqID); err != nil {
		t.Fatalf("原始层应有数据: %v", err)
	}
	if _, err := s.q.GetSnapshotByRequestID(ctx, reqID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("事实层应为空, got err = %v", err)
	}
	// 快照缺失的请求不归入任何会话。
	summaries, err := s.ListSessionSummaries(ctx, "", "", "", 10)
	if err != nil || len(summaries) != 0 {
		t.Fatalf("会话列表 = %v, %v; want 空", summaries, err)
	}
}

// TestStore_SaveTraceMissingRequestFails 覆盖外键约束:记录不存在时轨迹写入失败。
func TestStore_SaveTraceMissingRequestFails(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()
	body, snap := traceFixture(4242) // 不存在的 request_id

	if err := s.SaveTrace(ctx, body, snap); err == nil {
		t.Fatal("SaveTrace() error = nil, want 外键约束失败")
	}
	summaries, err := s.ListSessionSummaries(ctx, "", "", "", 10)
	if err != nil || len(summaries) != 0 {
		t.Fatalf("失败后不应留下轨迹: %v, %v", summaries, err)
	}
}
