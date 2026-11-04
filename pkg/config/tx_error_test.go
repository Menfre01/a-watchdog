package config

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"modernc.org/sqlite"

	"awatchdog/pkg/config/db"
)

// errInjected 是测试注入的失败原因,用于断言错误链保留了原始错误。
var errInjected = errors.New("injected failure")

// hookConfig 声明要注入的失败点。零值表示不注入。
// 这些失败点无法用真实 SQLite 稳定触发(驱动不返回这些错误),
// 因此用包装连接器在驱动边界注入,以覆盖生产代码的错误分支。
type hookConfig struct {
	beginErr        error
	commitErr       error
	closeErr        error
	lastInsertIDErr error
	execContains    string
	execErr         error
}

// hookConnector 返回真实 SQLite 连接,并按 hookConfig 包装失败点。
type hookConnector struct {
	dsn string
	cfg *hookConfig
}

func (c *hookConnector) Connect(context.Context) (driver.Conn, error) {
	inner, err := (&sqlite.Driver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &hookConn{Conn: inner, cfg: c.cfg}, nil
}

func (c *hookConnector) Driver() driver.Driver { return &sqlite.Driver{} }

// hookConn 转发到真实连接,仅在配置的失败点返回注入错误。
type hookConn struct {
	driver.Conn
	cfg *hookConfig
}

func (c *hookConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.cfg.beginErr != nil {
		return nil, c.cfg.beginErr
	}
	inner, ok := c.Conn.(driver.ConnBeginTx)
	if !ok {
		return nil, driver.ErrSkip
	}
	tx, err := inner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &hookTx{Tx: tx, cfg: c.cfg}, nil
}

func (c *hookConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.cfg.execErr != nil && strings.Contains(query, c.cfg.execContains) {
		return nil, c.cfg.execErr
	}
	inner, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	res, err := inner.ExecContext(ctx, query, args)
	if err != nil || res == nil || c.cfg.lastInsertIDErr == nil {
		return res, err
	}
	return hookResult{Result: res, err: c.cfg.lastInsertIDErr}, nil
}

func (c *hookConn) Close() error {
	err := c.Conn.Close()
	if c.cfg.closeErr != nil {
		return c.cfg.closeErr
	}
	return err
}

// hookTx 在 Commit 时注入失败;先回滚底层事务,避免连接带着未结束的事务回到池中。
type hookTx struct {
	driver.Tx
	cfg *hookConfig
}

func (t *hookTx) Commit() error {
	if t.cfg.commitErr != nil {
		// 测试注入路径:先回滚,保证后续断言看到的是回滚后的库状态。
		_ = t.Rollback()
		return t.cfg.commitErr
	}
	return t.Tx.Commit()
}

// hookResult 只覆盖 LastInsertId,其余行为转发真实结果。
type hookResult struct {
	driver.Result
	err error
}

func (r hookResult) LastInsertId() (int64, error) { return 0, r.err }

// newHookStore 打开一个执行完迁移的真实 SQLite 库;cfg 用于后续注入失败。
// 调用方必须在建库后改 cfg 字段:建库期间的迁移会经过同一包装器。
func newHookStore(t *testing.T, cfg *hookConfig) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hook.db")
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	sqlDB := sql.OpenDB(&hookConnector{dsn: dsn, cfg: cfg})
	sqlDB.SetMaxOpenConns(1)
	if err := migrate(context.Background(), sqlDB); err != nil {
		t.Fatalf("migrate() error = %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() }) // 测试收尾:尽力释放,错误不参与断言
	return &Store{sqlDB: sqlDB, q: db.New(sqlDB)}
}

// TestStore_CloseError 覆盖 Close 在驱动关闭失败时的错误包装。
func TestStore_CloseError(t *testing.T) {
	cfg := &hookConfig{}
	s := newHookStore(t, cfg)
	ctx := context.Background()
	// 先建立一条池内连接,否则 Close 没有可关闭的连接,不会失败。
	if _, err := s.ListProviders(ctx); err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	cfg.closeErr = errInjected
	if err := s.Close(); !errors.Is(err, errInjected) {
		t.Fatalf("Close() error = %v, want 注入错误", err)
	}
}

// TestStore_LastInsertIDError 覆盖两个 Create 在自增 id 回取失败时的错误分支。
func TestStore_LastInsertIDError(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateProvider", func(t *testing.T) {
		cfg := &hookConfig{}
		s := newHookStore(t, cfg)
		cfg.lastInsertIDErr = errInjected
		params := db.CreateProviderParams{Name: "p", Protocol: "openai-chat", BaseUrl: "x", ApiKey: "k", Enabled: 1}
		if _, err := s.CreateProvider(ctx, params); !errors.Is(err, errInjected) {
			t.Fatalf("CreateProvider() error = %v, want 注入错误", err)
		}
	})

	t.Run("CreateModel", func(t *testing.T) {
		cfg := &hookConfig{}
		s := newHookStore(t, cfg)
		p := mustProvider(t, s, "p")
		cfg.lastInsertIDErr = errInjected
		params := db.CreateModelParams{Name: "m", ProviderID: p.ID, UpstreamModel: "u", Enabled: 1}
		if _, err := s.CreateModel(ctx, params); !errors.Is(err, errInjected) {
			t.Fatalf("CreateModel() error = %v, want 注入错误", err)
		}
	})
}

// TestStore_CommitErrors 覆盖两个事务方法在提交失败时的错误分支与回滚效果。
func TestStore_CommitErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("InsertRequests", func(t *testing.T) {
		cfg := &hookConfig{}
		s := newHookStore(t, cfg)
		cfg.commitErr = errInjected
		_, err := s.InsertRequests(ctx, []db.InsertRequestParams{
			{Ts: "2026-10-13T09:00:00Z", Protocol: "anthropic-messages", StatusCode: 200},
		})
		if !errors.Is(err, errInjected) {
			t.Fatalf("InsertRequests() error = %v, want 注入错误", err)
		}
		cfg.commitErr = nil
		if n, err := s.CountRequests(ctx); err != nil || n != 0 {
			t.Fatalf("提交失败后仍有记录: count = %d, %v", n, err)
		}
	})

	t.Run("SaveTrace", func(t *testing.T) {
		cfg := &hookConfig{}
		s := newHookStore(t, cfg)
		reqID := insertTraceRequest(t, s, "2026-10-13T09:00:00Z", 1)
		body, snap := traceFixture(reqID)
		cfg.commitErr = errInjected
		if err := s.SaveTrace(ctx, body, snap); !errors.Is(err, errInjected) {
			t.Fatalf("SaveTrace() error = %v, want 注入错误", err)
		}
		cfg.commitErr = nil
		// 回滚:原始层与事实层都不得留下数据。
		if _, err := s.q.GetRequestBodyByRequestID(ctx, reqID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("原始层残留半条轨迹, err = %v", err)
		}
		if snaps, err := s.ListSnapshotsBySession(ctx, snap.SessionID); err != nil || len(snaps) != 0 {
			t.Fatalf("事实层残留数据: %d, %v", len(snaps), err)
		}
	})
}

// TestApplyMigration_Errors 覆盖单条迁移的 DDL 失败、版本推进失败与提交失败。
func TestApplyMigration_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("ExecDDLError", func(t *testing.T) {
		s := newHookStore(t, &hookConfig{})
		if err := applyMigration(ctx, s.sqlDB, 42, "NOT VALID SQL"); err == nil {
			t.Fatal("applyMigration(bad ddl) error = nil, want 执行失败")
		}
	})

	t.Run("SetUserVersionError", func(t *testing.T) {
		cfg := &hookConfig{}
		s := newHookStore(t, cfg)
		cfg.execContains, cfg.execErr = "PRAGMA user_version", errInjected
		if err := applyMigration(ctx, s.sqlDB, 42, "CREATE TABLE t42 (id INTEGER)"); !errors.Is(err, errInjected) {
			t.Fatalf("applyMigration() error = %v, want 注入错误", err)
		}
		cfg.execErr = nil
		// 原子性:版本推进失败时 DDL 一并回滚。
		var n int
		if err := s.sqlDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE name = 't42'").Scan(&n); err != nil {
			t.Fatalf("count sqlite_master: %v", err)
		}
		if n != 0 {
			t.Fatalf("失败迁移留下了表 t42")
		}
	})

	t.Run("CommitError", func(t *testing.T) {
		cfg := &hookConfig{}
		s := newHookStore(t, cfg)
		cfg.commitErr = errInjected
		if err := applyMigration(ctx, s.sqlDB, 42, "CREATE TABLE t43 (id INTEGER)"); !errors.Is(err, errInjected) {
			t.Fatalf("applyMigration() error = %v, want 注入错误", err)
		}
		cfg.commitErr = nil
		// 版本不推进:提交失败等价于该迁移未应用。
		var version int
		if err := s.sqlDB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			t.Fatalf("read user_version: %v", err)
		}
		if version != 7 {
			t.Fatalf("user_version = %d, want 7", version)
		}
	})
}

// TestMigrate_IdempotentAndSkipsApplied 覆盖已应用版本的跳过路径:
// 重复执行返回 nil,版本与数据都不变。
func TestMigrate_IdempotentAndSkipsApplied(t *testing.T) {
	s := newHookStore(t, &hookConfig{})
	ctx := context.Background()
	if err := migrate(ctx, s.sqlDB); err != nil {
		t.Fatalf("second migrate() error = %v", err)
	}
	var version int
	if err := s.sqlDB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 7 {
		t.Fatalf("user_version = %d, want 7", version)
	}
	// 已有数据在重复迁移后保留。
	if _, err := s.CreateProvider(ctx, db.CreateProviderParams{
		Name: "p", Protocol: "openai-chat", BaseUrl: "x", ApiKey: "k", Enabled: 1,
	}); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	if err := migrate(ctx, s.sqlDB); err != nil {
		t.Fatalf("third migrate() error = %v", err)
	}
	if providers, err := s.ListProviders(ctx); err != nil || len(providers) != 1 {
		t.Fatalf("ListProviders() = %v, %v; want 1 条", providers, err)
	}
}
