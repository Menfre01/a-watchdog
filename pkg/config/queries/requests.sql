-- Query set for the requests log. Keep ASCII-only (see providers.sql note).

-- name: InsertRequest :exec
INSERT INTO requests (
    ts, client_key_id, client_key_name, protocol, requested_model,
    provider_id, upstream_model, status_code, latency_ms, stream,
    usage_in, usage_out, usage_cache, error_kind
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CountRequests :one
SELECT COUNT(*) FROM requests;

-- name: ListRecentRequests :many
SELECT * FROM requests ORDER BY id DESC LIMIT ?;
