-- Query set for the requests log. Keep ASCII-only (see providers.sql note).

-- name: InsertRequest :exec
INSERT INTO requests (
    ts, client_key_id, client_key_name, protocol, requested_model,
    provider_id, upstream_model, status_code, latency_ms, stream,
    usage_in, usage_out, usage_cache, error_kind
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertRequestReturningID :one
INSERT INTO requests (
    ts, client_key_id, client_key_name, protocol, requested_model,
    provider_id, upstream_model, status_code, latency_ms, stream,
    usage_in, usage_out, usage_cache, error_kind
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: CountRequests :one
SELECT COUNT(*) FROM requests;

-- name: ListRecentRequests :many
SELECT * FROM requests ORDER BY id DESC LIMIT ?;

-- name: AggregateRequestsSince :one
SELECT
    COUNT(*) AS count,
    CAST(COALESCE(SUM(CASE WHEN status_code >= 400 OR error_kind <> '' THEN 1 ELSE 0 END), 0) AS INTEGER) AS errors,
    CAST(COALESCE(SUM(usage_in), 0) AS INTEGER) AS usage_in,
    CAST(COALESCE(SUM(usage_out), 0) AS INTEGER) AS usage_out,
    CAST(COALESCE(SUM(usage_cache), 0) AS INTEGER) AS usage_cache
FROM requests
WHERE ts >= ?;

-- name: ListRequestLatenciesSince :many
SELECT latency_ms FROM requests WHERE ts >= ? ORDER BY latency_ms;
