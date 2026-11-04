-- 0006_response_bodies.sql —— 上游响应原文(设计依据:openspec/changes/response-capture)。
-- 只存响应体本身;请求行与入向头仍在 request_bodies。
CREATE TABLE response_bodies (
    request_id  INTEGER PRIMARY KEY REFERENCES requests(id),
    body_gzip   BLOB    NOT NULL,
    truncated   INTEGER NOT NULL DEFAULT 0,
    complete    INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
