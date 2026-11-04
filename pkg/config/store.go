// Package config 提供配置与请求记录的 SQLite 存储,以及请求路径只读的进程内一致性快照。
// 数据访问由 sqlc 生成(pkg/config/db,见 design.md D4/D5)。
package config

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"

	"ds-context/pkg/config/db"

	_ "modernc.org/sqlite" // 注册纯 Go SQLite 驱动(免 cgo,保单二进制分发)
)

// ErrNotFound 表示目标配置实体或键不存在。
var ErrNotFound = errors.New("config: not found")

// Store 是配置存储入口:持有 SQLite 连接、sqlc 查询器与当前快照。
type Store struct {
	sqlDB *sql.DB
	q     *db.Queries
	snap  atomic.Pointer[Snapshot]
}

// Open 打开(必要时创建)数据文件:应用 PRAGMA、执行内嵌迁移、构建初始快照。
func Open(path string) (*Store, error) {
	// _pragma 参数由驱动在每个新连接上应用,等价于连接级初始化。
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("config: open %s: %w", path, err)
	}
	// SQLite 单写者模型:限制单连接,用最直观的形态保证并发安全(个人量级性能充足)。
	sqlDB.SetMaxOpenConns(1)
	// 启动路径无请求上下文,统一使用 Background;迁移与初始快照加载均经它。
	ctx := context.Background()
	if err := migrate(ctx, sqlDB); err != nil {
		_ = sqlDB.Close() // 失败路径:尽力释放,优先返回原始错误
		return nil, err
	}
	s := &Store{sqlDB: sqlDB, q: db.New(sqlDB)}
	if err := s.Refresh(ctx); err != nil {
		_ = sqlDB.Close() // 同上
		return nil, err
	}
	return s, nil
}

// Close 关闭底层连接。
func (s *Store) Close() error {
	if err := s.sqlDB.Close(); err != nil {
		return fmt.Errorf("config: close: %w", err)
	}
	return nil
}

// Refresh 从数据库重建快照并原子替换;所有写方法在落库成功后调用它,
// 旧视图保持不变(单次请求内视图一致)。
func (s *Store) Refresh(ctx context.Context) error {
	providers, err := s.q.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("config: refresh providers: %w", err)
	}
	models, err := s.q.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("config: refresh models: %w", err)
	}
	keys, err := s.q.ListClientKeys(ctx)
	if err != nil {
		return fmt.Errorf("config: refresh client_keys: %w", err)
	}
	settings, err := s.q.ListSettings(ctx)
	if err != nil {
		return fmt.Errorf("config: refresh settings: %w", err)
	}
	snap := &Snapshot{
		providers:    make(map[int64]ProviderInfo, len(providers)),
		modelsByName: make(map[string]ModelInfo, len(models)),
		keysByHash:   make(map[string]KeyInfo, len(keys)),
		settings:     make(map[string]string, len(settings)),
	}
	for _, p := range providers {
		snap.providers[p.ID] = ProviderInfo{
			ID:         p.ID,
			Name:       p.Name,
			Protocol:   p.Protocol,
			BaseURL:    p.BaseUrl,
			APIKey:     p.ApiKey,
			ProbeModel: p.ProbeModel,
			Enabled:    p.Enabled != 0,
		}
	}
	for _, m := range models {
		snap.modelsByName[m.Name] = ModelInfo{
			Name:          m.Name,
			ProviderID:    m.ProviderID,
			UpstreamModel: m.UpstreamModel,
			Enabled:       m.Enabled != 0,
		}
	}
	for _, k := range keys {
		snap.keysByHash[k.KeyHash] = KeyInfo{ID: k.ID, Name: k.Name, Enabled: k.Enabled != 0}
	}
	for _, st := range settings {
		snap.settings[st.Key] = st.Value
	}
	s.snap.Store(snap)
	return nil
}

// Snapshot 返回当前快照;调用方只读,不持锁。
func (s *Store) Snapshot() *Snapshot {
	return s.snap.Load()
}

// BoolToInt 将 bool 转为存储使用的 0/1。
func BoolToInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// ---- providers ----

// ListProviders 返回全部上游接入。
func (s *Store) ListProviders(ctx context.Context) ([]db.Provider, error) {
	rows, err := s.q.ListProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("config: list providers: %w", err)
	}
	return rows, nil
}

// CreateProvider 新建上游接入,成功后刷新快照。
func (s *Store) CreateProvider(ctx context.Context, p db.CreateProviderParams) (db.Provider, error) {
	res, err := s.q.CreateProvider(ctx, p)
	if err != nil {
		return db.Provider{}, fmt.Errorf("config: create provider: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return db.Provider{}, fmt.Errorf("config: create provider id: %w", err)
	}
	if err := s.Refresh(ctx); err != nil {
		return db.Provider{}, err
	}
	row, err := s.q.GetProvider(ctx, id)
	if err != nil {
		return db.Provider{}, fmt.Errorf("config: get new provider: %w", err)
	}
	return row, nil
}

// UpdateProvider 更新上游接入,成功后刷新快照。
func (s *Store) UpdateProvider(ctx context.Context, p db.UpdateProviderParams) error {
	if err := s.q.UpdateProvider(ctx, p); err != nil {
		return fmt.Errorf("config: update provider: %w", err)
	}
	return s.Refresh(ctx)
}

// DeleteProvider 删除上游接入,成功后刷新快照。
func (s *Store) DeleteProvider(ctx context.Context, id int64) error {
	if err := s.q.DeleteProvider(ctx, id); err != nil {
		return fmt.Errorf("config: delete provider: %w", err)
	}
	return s.Refresh(ctx)
}

// GetProvider 按 id 返回上游接入;不存在时返回 ErrNotFound。
func (s *Store) GetProvider(ctx context.Context, id int64) (db.Provider, error) {
	row, err := s.q.GetProvider(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return db.Provider{}, fmt.Errorf("%w: provider %d", ErrNotFound, id)
	}
	if err != nil {
		return db.Provider{}, fmt.Errorf("config: get provider: %w", err)
	}
	return row, nil
}

// ---- models ----

// ListModels 返回全部模型目录条目。
func (s *Store) ListModels(ctx context.Context) ([]db.Model, error) {
	rows, err := s.q.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("config: list models: %w", err)
	}
	return rows, nil
}

// CreateModel 新建模型目录条目,成功后刷新快照。
func (s *Store) CreateModel(ctx context.Context, m db.CreateModelParams) (db.Model, error) {
	res, err := s.q.CreateModel(ctx, m)
	if err != nil {
		return db.Model{}, fmt.Errorf("config: create model: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return db.Model{}, fmt.Errorf("config: create model id: %w", err)
	}
	if err := s.Refresh(ctx); err != nil {
		return db.Model{}, err
	}
	row, err := s.q.GetModel(ctx, id)
	if err != nil {
		return db.Model{}, fmt.Errorf("config: get new model: %w", err)
	}
	return row, nil
}

// UpdateModel 更新模型目录条目,成功后刷新快照。
func (s *Store) UpdateModel(ctx context.Context, m db.UpdateModelParams) error {
	if err := s.q.UpdateModel(ctx, m); err != nil {
		return fmt.Errorf("config: update model: %w", err)
	}
	return s.Refresh(ctx)
}

// DeleteModel 删除模型目录条目,成功后刷新快照。
func (s *Store) DeleteModel(ctx context.Context, id int64) error {
	if err := s.q.DeleteModel(ctx, id); err != nil {
		return fmt.Errorf("config: delete model: %w", err)
	}
	return s.Refresh(ctx)
}

// ---- client keys ----

// ListClientKeys 返回全部客户端密钥(不含明文)。
func (s *Store) ListClientKeys(ctx context.Context) ([]db.ClientKey, error) {
	rows, err := s.q.ListClientKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("config: list client_keys: %w", err)
	}
	return rows, nil
}

// CreateClientKey 新建客户端密钥(存哈希与展示前缀),成功后刷新快照。
func (s *Store) CreateClientKey(ctx context.Context, p db.CreateClientKeyParams) (db.ClientKey, error) {
	if _, err := s.q.CreateClientKey(ctx, p); err != nil {
		return db.ClientKey{}, fmt.Errorf("config: create client_key: %w", err)
	}
	if err := s.Refresh(ctx); err != nil {
		return db.ClientKey{}, err
	}
	row, err := s.q.GetClientKeyByHash(ctx, p.KeyHash)
	if err != nil {
		return db.ClientKey{}, fmt.Errorf("config: get new client_key: %w", err)
	}
	return row, nil
}

// SetClientKeyEnabled 启用/停用客户端密钥,成功后刷新快照(停用即时生效)。
func (s *Store) SetClientKeyEnabled(ctx context.Context, id int64, enabled bool) error {
	params := db.SetClientKeyEnabledParams{ID: id, Enabled: BoolToInt(enabled)}
	if err := s.q.SetClientKeyEnabled(ctx, params); err != nil {
		return fmt.Errorf("config: set client_key enabled: %w", err)
	}
	return s.Refresh(ctx)
}

// DeleteClientKey 删除客户端密钥,成功后刷新快照(被删密钥即时失效)。
func (s *Store) DeleteClientKey(ctx context.Context, id int64) error {
	res, err := s.q.DeleteClientKey(ctx, id)
	if err != nil {
		return fmt.Errorf("config: delete client_key: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("config: delete client_key rows: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return s.Refresh(ctx)
}

// InsertResponseBody 写入上游响应原文(gzip)。
func (s *Store) InsertResponseBody(ctx context.Context, arg db.InsertResponseBodyParams) error {
	if err := s.q.InsertResponseBody(ctx, arg); err != nil {
		return fmt.Errorf("config: insert response body: %w", err)
	}
	return nil
}

// ResponseBody 读取某请求的响应原文;无记录时返回错误。
func (s *Store) ResponseBody(ctx context.Context, requestID int64) (db.ResponseBody, error) {
	row, err := s.q.GetResponseBody(ctx, requestID)
	if err != nil {
		return db.ResponseBody{}, fmt.Errorf("config: get response body: %w", err)
	}
	return row, nil
}

// ---- request metrics ----

// AggregateRequestsSince 汇总某时刻之后的请求数、错误数与用量。
func (s *Store) AggregateRequestsSince(ctx context.Context, since string) (db.AggregateRequestsSinceRow, error) {
	row, err := s.q.AggregateRequestsSince(ctx, since)
	if err != nil {
		return db.AggregateRequestsSinceRow{}, fmt.Errorf("config: aggregate requests: %w", err)
	}
	return row, nil
}

// ListRequestLatenciesSince 返回某时刻之后的全部延迟,升序,供分位数计算。
func (s *Store) ListRequestLatenciesSince(ctx context.Context, since string) ([]int64, error) {
	rows, err := s.q.ListRequestLatenciesSince(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("config: list request latencies: %w", err)
	}
	return rows, nil
}

// ---- tool events ----

// InsertToolEvent 写入一条工具事件;(client, tool_call_id, phase) 重复时忽略。
func (s *Store) InsertToolEvent(ctx context.Context, p db.InsertToolEventParams) error {
	if err := s.q.InsertToolEvent(ctx, p); err != nil {
		return fmt.Errorf("config: insert tool_event: %w", err)
	}
	return nil
}

// ListToolEvents 返回某会话的全部工具事件,按时间与自增序排列。
func (s *Store) ListToolEvents(ctx context.Context, sessionID string) ([]db.ToolEvent, error) {
	rows, err := s.q.ListToolEvents(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("config: list tool_events: %w", err)
	}
	return rows, nil
}

// ---- prompt events ----

// InsertPromptEvent 写入一条提示提交事件;有 prompt_id 时重复上报被忽略。
func (s *Store) InsertPromptEvent(ctx context.Context, p db.InsertPromptEventParams) error {
	if err := s.q.InsertPromptEvent(ctx, p); err != nil {
		return fmt.Errorf("config: insert prompt_event: %w", err)
	}
	return nil
}

// ListPromptEvents 返回某会话的提交事件,按时间升序。
func (s *Store) ListPromptEvents(ctx context.Context, sessionID string) ([]db.PromptEvent, error) {
	rows, err := s.q.ListPromptEvents(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("config: list prompt_events: %w", err)
	}
	return rows, nil
}

// ---- settings ----

// ListSettings 返回全部全局配置项。
func (s *Store) ListSettings(ctx context.Context) ([]db.Setting, error) {
	rows, err := s.q.ListSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("config: list settings: %w", err)
	}
	return rows, nil
}

// GetSetting 读取单个配置项;不存在时返回 ErrNotFound。
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	row, err := s.q.GetSetting(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: setting %q", ErrNotFound, key)
	}
	if err != nil {
		return "", fmt.Errorf("config: get setting: %w", err)
	}
	return row.Value, nil
}

// UpsertSetting 写入或覆盖配置项,成功后刷新快照。
func (s *Store) UpsertSetting(ctx context.Context, key, value string) error {
	if err := s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: key, Value: value}); err != nil {
		return fmt.Errorf("config: upsert setting: %w", err)
	}
	return s.Refresh(ctx)
}

// ---- requests ----

// InsertRequests 在单事务内逐条写入请求记录,并返回与入参一一对应的自增 id。
// 逐条 `INSERT ... RETURNING id` 消除了「按 id 倒序回取」对写入时序的隐式依赖,
// 也让记录 id 与捕捉队列元素一一对应(design.md D2)。
func (s *Store) InsertRequests(ctx context.Context, reqs []db.InsertRequestParams) ([]int64, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("config: begin requests tx: %w", err)
	}
	// 提交成功后 Rollback 为无操作,此处显式忽略其错误。
	defer func() { _ = tx.Rollback() }()

	qtx := s.q.WithTx(tx)
	ids := make([]int64, 0, len(reqs))
	for _, r := range reqs {
		id, err := qtx.InsertRequestReturningID(ctx, db.InsertRequestReturningIDParams(r))
		if err != nil {
			return nil, fmt.Errorf("config: insert request: %w", err)
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("config: commit requests: %w", err)
	}
	return ids, nil
}

// CountRequests 返回请求记录总数(控制台概览与测试用)。
func (s *Store) CountRequests(ctx context.Context) (int64, error) {
	n, err := s.q.CountRequests(ctx)
	if err != nil {
		return 0, fmt.Errorf("config: count requests: %w", err)
	}
	return n, nil
}

// ListRecentRequests 返回最近的请求记录(按 id 倒序)。
func (s *Store) ListRecentRequests(ctx context.Context, limit int64) ([]db.Request, error) {
	rows, err := s.q.ListRecentRequests(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("config: list recent requests: %w", err)
	}
	return rows, nil
}

// ---- 轨迹(原始层与事实层,见 design.md D1) ----

// TraceSnapshot 是一次请求的规范化快照参数及其派生列。
// Seq 由调用方按会话递增(会话内自 1 起);SessionSource/SessionNote 记录判定来源与理由。
type TraceSnapshot struct {
	SessionID     string
	SessionSource string
	SessionNote   string
	Seq           int64
	Ts            string
	NdjsonGzip    []byte
	LineHashes    []byte
	LineSizes     []byte
}

// InsertRequestBody 写入原始层:请求行、脱敏后的入向请求头与 body 原文(gzip)。
func (s *Store) InsertRequestBody(ctx context.Context, arg db.InsertRequestBodyParams) error {
	if err := s.q.InsertRequestBody(ctx, arg); err != nil {
		return fmt.Errorf("config: insert request body: %w", err)
	}
	return nil
}

// InsertSnapshot 写入事实层的一条规范化快照。
func (s *Store) InsertSnapshot(ctx context.Context, arg db.InsertSnapshotParams) error {
	if err := s.q.InsertSnapshot(ctx, arg); err != nil {
		return fmt.Errorf("config: insert snapshot: %w", err)
	}
	return nil
}

// SaveTrace 在单个事务内写入一次请求的轨迹:原始层必有,快照可选(请求体不可解析、
// 辅助端点时不产快照)。轨迹与请求记录是两个独立事务——轨迹失败只回滚该请求的轨迹,
// 已提交的记录不受影响(design.md D2 的失败隔离)。
func (s *Store) SaveTrace(ctx context.Context, body db.InsertRequestBodyParams, snap *TraceSnapshot) error {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("config: begin trace tx: %w", err)
	}
	// 提交成功后 Rollback 为无操作,此处显式忽略其错误。
	defer func() { _ = tx.Rollback() }()

	qtx := s.q.WithTx(tx)
	if err := qtx.InsertRequestBody(ctx, body); err != nil {
		return fmt.Errorf("config: insert request body: %w", err)
	}
	if snap != nil {
		if err := qtx.InsertSnapshot(ctx, db.InsertSnapshotParams{
			RequestID:     body.RequestID,
			SessionID:     snap.SessionID,
			SessionSource: snap.SessionSource,
			SessionNote:   snap.SessionNote,
			Seq:           snap.Seq,
			Ts:            snap.Ts,
			NdjsonGzip:    snap.NdjsonGzip,
			LineHashes:    snap.LineHashes,
			LineSizes:     snap.LineSizes,
		}); err != nil {
			return fmt.Errorf("config: insert snapshot: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("config: commit trace: %w", err)
	}
	return nil
}

// NextSnapshotSeq 返回某会话的下一个序号(自 1 起);捕捉侧冷启动时用它补齐序号。
func (s *Store) NextSnapshotSeq(ctx context.Context, sessionID string) (int64, error) {
	seq, err := s.q.NextSnapshotSeq(ctx, sessionID)
	if err != nil {
		return 0, fmt.Errorf("config: next snapshot seq: %w", err)
	}
	return seq, nil
}

// ListSnapshotsBySession 按序号返回一个会话的全部快照(含压缩后的 BLOB)。
func (s *Store) ListSnapshotsBySession(ctx context.Context, sessionID string) ([]db.Snapshot, error) {
	rows, err := s.q.ListSnapshotsBySession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("config: list snapshots: %w", err)
	}
	return rows, nil
}

// ListSessionSteps 返回一个会话的全部 step:快照派生列与请求记录字段一次取齐,
// 供轨迹骨架与详情组装(按序号升序)。
func (s *Store) ListSessionSteps(ctx context.Context, sessionID string) ([]db.ListSessionStepsRow, error) {
	rows, err := s.q.ListSessionSteps(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("config: list session steps: %w", err)
	}
	return rows, nil
}

// SessionSummary 是会话列表的一行(见 design.md D6)。
// Steps 即该会话的请求数:会话归属由快照承载,二者恒等。
type SessionSummary struct {
	SessionID     string
	SessionSource string
	SessionNote   string
	FirstSeen     string
	LastSeen      string
	Steps         int64
	UsageIn       int64
	UsageOut      int64
	UsageCache    int64
}

// ListSessionSummaries 返回会话列表:keyID 过滤密钥维度,prefix 匹配客户端原始标识前缀,
// from/to 为时间范围;留空表示不过滤。过滤语义为「会话内有任一步落在时间范围内即入选」。
func (s *Store) ListSessionSummaries(ctx context.Context, keyID, prefix, from, to string, rowLimit int64) ([]SessionSummary, error) {
	rows, err := s.q.ListSessionSummaries(ctx, db.ListSessionSummariesParams{
		KeyID:    nullableString(keyID),
		Prefix:   nullableString(prefix),
		FromTs:   nullableString(from),
		ToTs:     nullableString(to),
		RowLimit: rowLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("config: list session summaries: %w", err)
	}
	out := make([]SessionSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, SessionSummary{
			SessionID:     r.SessionID,
			SessionSource: r.SessionSource,
			SessionNote:   r.SessionNote,
			FirstSeen:     r.FirstSeen,
			LastSeen:      r.LastSeen,
			Steps:         r.Steps,
			UsageIn:       r.UsageIn,
			UsageOut:      r.UsageOut,
			UsageCache:    r.UsageCache,
		})
	}
	return out, nil
}

// nullableString 把空串转成 SQL NULL:这些过滤参数里空串表示「不过滤」。
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
