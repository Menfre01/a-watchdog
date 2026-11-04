-- 0005_prompt_events.sql —— 提示提交采集(设计依据:openspec/changes/prompt-events-ingest)。
-- 只存提交时刻与提示长度,不存正文。
CREATE TABLE prompt_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    client     TEXT    NOT NULL,
    session_id TEXT    NOT NULL,
    turn_id    TEXT    NOT NULL DEFAULT '',
    prompt_id  TEXT    NOT NULL DEFAULT '',
    prompt_len INTEGER NOT NULL DEFAULT 0,
    ts         TEXT    NOT NULL,
    received_at TEXT   NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_prompt_events_session ON prompt_events (session_id, ts);

-- 有 prompt_id 时去重;无 id 的提交按每条记录。
CREATE UNIQUE INDEX idx_prompt_events_dedup
    ON prompt_events (client, session_id, prompt_id)
    WHERE prompt_id <> '';
