package config

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"awatchdog/pkg/config/db"
)

// TestStore_ClosedErrors_Extended 覆盖新增读写方法在底层连接不可用时的错误分支。
func TestStore_ClosedErrors_Extended(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	ctx := context.Background()
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	cases := []struct {
		name string
		call func() error
	}{
		{"DeleteClientKey", func() error { return s.DeleteClientKey(ctx, 1) }},
		{"InsertResponseBody", func() error {
			return s.InsertResponseBody(ctx, db.InsertResponseBodyParams{RequestID: 1, BodyGzip: []byte{0}})
		}},
		{"ResponseBody", func() error { _, err := s.ResponseBody(ctx, 1); return err }},
		{"AggregateRequestsSince", func() error {
			_, err := s.AggregateRequestsSince(ctx, "2026-01-01T00:00:00Z")
			return err
		}},
		{"ListRequestLatenciesSince", func() error {
			_, err := s.ListRequestLatenciesSince(ctx, "2026-01-01T00:00:00Z")
			return err
		}},
		{"InsertToolEvent", func() error {
			return s.InsertToolEvent(ctx, db.InsertToolEventParams{Client: "c", Phase: "start"})
		}},
		{"ListToolEvents", func() error { _, err := s.ListToolEvents(ctx, "1:s"); return err }},
		{"InsertPromptEvent", func() error {
			return s.InsertPromptEvent(ctx, db.InsertPromptEventParams{Client: "c"})
		}},
		{"ListPromptEvents", func() error { _, err := s.ListPromptEvents(ctx, "1:s"); return err }},
		{"InsertRequestBody", func() error {
			return s.InsertRequestBody(ctx, db.InsertRequestBodyParams{RequestID: 1})
		}},
		{"InsertSnapshot", func() error {
			return s.InsertSnapshot(ctx, db.InsertSnapshotParams{RequestID: 1, SessionID: "1:s"})
		}},
		{"SaveTrace", func() error {
			body, snap := traceFixture(1)
			return s.SaveTrace(ctx, body, snap)
		}},
		{"NextSnapshotSeq", func() error { _, err := s.NextSnapshotSeq(ctx, "1:s"); return err }},
		{"ListSnapshotsBySession", func() error { _, err := s.ListSnapshotsBySession(ctx, "1:s"); return err }},
		{"ListSessionSteps", func() error { _, err := s.ListSessionSteps(ctx, "1:s"); return err }},
		{"ListSessionSummaries", func() error {
			_, err := s.ListSessionSummaries(ctx, "", "", "", "", 10)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatalf("%s: closed store error = nil, want error", tc.name)
			}
		})
	}
}

// TestStore_CreateReadBackFails 覆盖三个 Create 在「写入成功但回读不到」时的错误分支。
// 用测试内的 AFTER INSERT 触发器删除新行制造该状态,不改生产代码。
func TestStore_CreateReadBackFails(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateProvider", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.sqlDB.ExecContext(ctx,
			`CREATE TRIGGER del_new_provider AFTER INSERT ON providers
			 BEGIN DELETE FROM providers WHERE id = NEW.id; END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		params := db.CreateProviderParams{Name: "p", Protocol: "openai-chat", BaseUrl: "x", ApiKey: "k", Enabled: 1}
		if _, err := s.CreateProvider(ctx, params); err == nil {
			t.Fatal("CreateProvider() error = nil, want 回读失败")
		}
	})

	t.Run("CreateModel", func(t *testing.T) {
		s := newTestStore(t)
		p := mustProvider(t, s, "p")
		if _, err := s.sqlDB.ExecContext(ctx,
			`CREATE TRIGGER del_new_model AFTER INSERT ON models
			 BEGIN DELETE FROM models WHERE id = NEW.id; END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		params := db.CreateModelParams{Name: "m", ProviderID: p.ID, UpstreamModel: "u", Enabled: 1}
		if _, err := s.CreateModel(ctx, params); err == nil {
			t.Fatal("CreateModel() error = nil, want 回读失败")
		}
	})

	t.Run("CreateClientKey", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.sqlDB.ExecContext(ctx,
			`CREATE TRIGGER del_new_key AFTER INSERT ON client_keys
			 BEGIN DELETE FROM client_keys WHERE id = NEW.id; END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		params := db.CreateClientKeyParams{Name: "k", KeyHash: "h", KeyPrefix: "p", Enabled: 1}
		if _, err := s.CreateClientKey(ctx, params); err == nil {
			t.Fatal("CreateClientKey() error = nil, want 回读失败")
		}
	})
}

// TestOpen_InitialSnapshotFails 覆盖 Open 中初始快照构建失败的分支:
// 数据库声称版本已是最新(user_version=7)但表不存在,迁移全部跳过,刷新快照必然失败。
func TestOpen_InitialSnapshotFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v7.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	ctx := context.Background()
	if _, err := raw.ExecContext(ctx, "PRAGMA user_version = 7"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	if _, err := Open(path); err == nil {
		t.Fatal("Open(versioned empty db) error = nil, want 快照构建失败")
	}
}

// TestStore_TraceInsertConflicts 覆盖原始层与事实层的重复写入错误(读回路径由成功用例覆盖)。
func TestStore_TraceInsertConflicts(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()
	reqID := insertTraceRequest(t, s, "2026-10-13T09:00:00Z", 1)
	body, snap := traceFixture(reqID)

	if err := s.InsertRequestBody(ctx, body); err != nil {
		t.Fatalf("InsertRequestBody() error = %v", err)
	}
	// request_bodies 以 request_id 为主键:重复写入必须失败。
	if err := s.InsertRequestBody(ctx, body); err == nil {
		t.Fatal("InsertRequestBody(duplicate) error = nil, want 主键冲突")
	}

	snapParams := db.InsertSnapshotParams{
		RequestID:     reqID,
		SessionID:     snap.SessionID,
		SessionSource: snap.SessionSource,
		SessionNote:   snap.SessionNote,
		Seq:           snap.Seq,
		Ts:            snap.Ts,
		NdjsonGzip:    snap.NdjsonGzip,
		LineHashes:    snap.LineHashes,
		LineSizes:     snap.LineSizes,
	}
	if err := s.InsertSnapshot(ctx, snapParams); err != nil {
		t.Fatalf("InsertSnapshot() error = %v", err)
	}
	// snapshots.request_id 唯一:重复写入必须失败。
	if err := s.InsertSnapshot(ctx, snapParams); err == nil {
		t.Fatal("InsertSnapshot(duplicate) error = nil, want 唯一约束失败")
	}
}

// TestStore_InsertRequestsRollsBackOnInsertError 覆盖批量写入中途失败:
// 触发器拒绝插入时整事务回滚,不留半批记录。
func TestStore_InsertRequestsRollsBackOnInsertError(t *testing.T) {
	s := openTraceStore(t)
	ctx := context.Background()
	if _, err := s.sqlDB.ExecContext(ctx,
		`CREATE TRIGGER reject_requests BEFORE INSERT ON requests
		 BEGIN SELECT RAISE(ABORT, 'rejected by test trigger'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err := s.InsertRequests(ctx, []db.InsertRequestParams{
		{Ts: "2026-10-13T09:00:00Z", Protocol: "anthropic-messages", StatusCode: 200},
	})
	if err == nil {
		t.Fatal("InsertRequests() error = nil, want 触发器拒绝")
	}
	if n, err := s.CountRequests(ctx); err != nil || n != 0 {
		t.Fatalf("失败批次留下记录: count = %d, %v", n, err)
	}
}
