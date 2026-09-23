// Package config 提供配置与请求记录的 SQLite 存储,以及请求路径只读的进程内一致性快照。
// 数据访问由 sqlc 生成(pkg/config/db,见 design.md D4/D5)。
package config

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

// insertRequestsChunk 是批量插入的分块大小:200 行 × 14 列 = 2800 个占位符,
// 远低于 SQLite 变量数上限。
const insertRequestsChunk = 200

// insertRequestsCols 是 requests 表批量插入的列清单。
const insertRequestsCols = "ts, client_key_id, client_key_name, protocol, requested_model, " +
	"provider_id, upstream_model, status_code, latency_ms, stream, " +
	"usage_in, usage_out, usage_cache, error_kind"

// InsertRequests 批量写入请求记录(手写批量 INSERT;特殊路径按 design.md D4 绕过 sqlc)。
func (s *Store) InsertRequests(ctx context.Context, reqs []db.InsertRequestParams) error {
	for start := 0; start < len(reqs); start += insertRequestsChunk {
		end := min(start+insertRequestsChunk, len(reqs))
		if err := s.insertRequestsPart(ctx, reqs[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// insertRequestsPart 写入一批(不超过 insertRequestsChunk 行)记录。
func (s *Store) insertRequestsPart(ctx context.Context, reqs []db.InsertRequestParams) error {
	var q strings.Builder
	q.WriteString("INSERT INTO requests (")
	q.WriteString(insertRequestsCols)
	q.WriteString(") VALUES ")
	args := make([]any, 0, len(reqs)*14)
	for i, r := range reqs {
		if i > 0 {
			q.WriteString(",")
		}
		q.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
		args = append(args,
			r.Ts, r.ClientKeyID, r.ClientKeyName, r.Protocol, r.RequestedModel,
			r.ProviderID, r.UpstreamModel, r.StatusCode, r.LatencyMs, r.Stream,
			r.UsageIn, r.UsageOut, r.UsageCache, r.ErrorKind)
	}
	if _, err := s.sqlDB.ExecContext(ctx, q.String(), args...); err != nil {
		return fmt.Errorf("config: insert requests: %w", err)
	}
	return nil
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
