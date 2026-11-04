-- Query set for tool_events. Keep ASCII-only (see providers.sql note).

-- name: InsertToolEvent :exec
INSERT INTO tool_events (
    client, session_id, turn_id, tool_call_id, tool_name, phase, status, boundary, approval_wait, ts,
    input_gzip, input_truncated, result_gzip, result_truncated
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (client, tool_call_id, phase) DO NOTHING;

-- name: ListToolEvents :many
SELECT * FROM tool_events WHERE session_id = ? ORDER BY ts, id;
