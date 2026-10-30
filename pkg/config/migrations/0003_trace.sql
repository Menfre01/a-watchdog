-- 0003_trace.sql —— 轨迹捕捉:原始层与事实层(设计依据:openspec/changes/trace-system/design.md D1)。
-- 两张表都与 requests 一一关联:记录侧已存在时,轨迹缺失(捕捉失败/丢弃)只表现为对应行不存在。
-- line_hashes / line_sizes 是与分类规则无关的确定性派生列,快照从原始层重放时一并重算。

-- 原始层:请求行 + 脱敏后的入向请求头 + body 原文(gzip)。不可再生,是一切的保真底。
CREATE TABLE request_bodies (
    request_id INTEGER PRIMARY KEY REFERENCES requests (id),
    method     TEXT NOT NULL,
    path       TEXT NOT NULL,
    query      TEXT NOT NULL,
    body_gzip  BLOB NOT NULL,
    headers    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 事实层:每请求一条规范化快照(ndjson,gzip)及其行指纹/行长度序列。
-- line_hashes:每行内容哈希(sha256 前 8 字节)按行序拼接;line_sizes:每行字节数(uint32 大端)按行序拼接。
CREATE TABLE snapshots (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id     INTEGER NOT NULL UNIQUE REFERENCES requests (id),
    session_id     TEXT    NOT NULL,
    session_source TEXT    NOT NULL,
    session_note   TEXT    NOT NULL,
    seq            INTEGER NOT NULL,
    ts             TEXT    NOT NULL,
    ndjson_gzip    BLOB    NOT NULL,
    line_hashes    BLOB    NOT NULL,
    line_sizes     BLOB    NOT NULL
);

CREATE INDEX idx_snapshots_session ON snapshots (session_id, seq);
