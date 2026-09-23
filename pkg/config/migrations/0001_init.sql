-- 0001_init.sql —— 初始 schema(设计依据:openspec/changes/gateway-core/design.md D4)。
-- 注意:迁移文件需保持 sqlc 可解析(以 DDL 为主);PRAGMA 等连接级设置在 Go 侧连接初始化。

CREATE TABLE providers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    protocol   TEXT    NOT NULL,
    base_url   TEXT    NOT NULL,
    api_key    TEXT    NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE models (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL UNIQUE,
    provider_id    INTEGER NOT NULL REFERENCES providers (id),
    upstream_model TEXT    NOT NULL,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE client_keys (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    key_hash   TEXT    NOT NULL UNIQUE,
    key_prefix TEXT    NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE requests (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              TEXT    NOT NULL,
    client_key_id   INTEGER,
    client_key_name TEXT    NOT NULL DEFAULT '',
    protocol        TEXT    NOT NULL,
    requested_model TEXT    NOT NULL DEFAULT '',
    provider_id     INTEGER,
    upstream_model  TEXT,
    status_code     INTEGER NOT NULL,
    latency_ms      INTEGER NOT NULL,
    stream          INTEGER NOT NULL DEFAULT 0,
    usage_in        INTEGER,
    usage_out       INTEGER,
    usage_cache     INTEGER,
    error_kind      TEXT
);

CREATE INDEX idx_requests_ts ON requests (ts);
