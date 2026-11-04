-- Query set for prompt_events. Keep ASCII-only (see providers.sql note).

-- name: InsertPromptEvent :exec
INSERT INTO prompt_events (client, session_id, turn_id, prompt_id, prompt_len, ts)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING;

-- name: ListPromptEvents :many
SELECT * FROM prompt_events WHERE session_id = ? ORDER BY ts, id;
