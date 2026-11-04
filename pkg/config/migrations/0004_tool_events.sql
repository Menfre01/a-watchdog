-- 0004_tool_events.sql —— 工具调用上报(设计依据:openspec/changes/tool-events-ingest/design.md)。
-- 客户端在工具执行前后各上报一次;服务端按 (client, tool_call_id, phase) 幂等入库,
-- 读取时把同一 tool_call_id 的 start / end 配成一条调用记录。
-- 时长口径是「两个事件到达网关的墙钟差」,不是纯执行时间:审批等待与 hook 分发都在其中。
CREATE TABLE tool_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    client       TEXT    NOT NULL,
    session_id   TEXT    NOT NULL,
    turn_id      TEXT    NOT NULL DEFAULT '',
    tool_call_id TEXT    NOT NULL,
    tool_name    TEXT    NOT NULL,
    phase        TEXT    NOT NULL CHECK (phase IN ('start', 'end')),
    status       TEXT    NOT NULL DEFAULT '',
    boundary     TEXT    NOT NULL DEFAULT 'hook',
    approval_wait INTEGER NOT NULL DEFAULT 0,
    ts           TEXT    NOT NULL,
    received_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE (client, tool_call_id, phase)
);

CREATE INDEX idx_tool_events_session ON tool_events (session_id, ts);
