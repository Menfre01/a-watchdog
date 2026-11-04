-- Query set for upstream responses. Keep ASCII-only.

-- name: InsertResponseBody :exec
INSERT INTO response_bodies (request_id, body_gzip, truncated, complete)
VALUES (?, ?, ?, ?)
ON CONFLICT (request_id) DO NOTHING;

-- name: GetResponseBody :one
SELECT request_id, body_gzip, truncated, complete, created_at
FROM response_bodies WHERE request_id = ?;
