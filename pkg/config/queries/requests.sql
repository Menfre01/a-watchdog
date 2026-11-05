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
    CAST(COALESCE(SUM(CASE WHEN status_code BETWEEN 400 AND 499 THEN 1 ELSE 0 END), 0) AS INTEGER) AS errors_4xx,
    CAST(COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0) AS INTEGER) AS errors_5xx,
    CAST(COALESCE(SUM(usage_in), 0) AS INTEGER) AS usage_in,
    CAST(COALESCE(SUM(usage_out), 0) AS INTEGER) AS usage_out,
    CAST(COALESCE(SUM(usage_cache), 0) AS INTEGER) AS usage_cache
FROM requests r
WHERE r.ts >= sqlc.arg('ts')
  AND (sqlc.narg('key_id') IS NULL OR r.client_key_id = sqlc.narg('key_id'))
  AND (
    sqlc.narg('session_prefix') IS NULL
    OR EXISTS (
      SELECT 1
      FROM snapshots s
      WHERE s.request_id = r.id
        AND substr(s.session_id, instr(s.session_id, ':') + 1) LIKE sqlc.narg('session_prefix') || '%'
    )
  );

-- name: ListRequestLatenciesSince :many
SELECT r.latency_ms
FROM requests r
WHERE r.ts >= sqlc.arg('ts')
  AND (sqlc.narg('key_id') IS NULL OR r.client_key_id = sqlc.narg('key_id'))
  AND (
    sqlc.narg('session_prefix') IS NULL
    OR EXISTS (
      SELECT 1
      FROM snapshots s
      WHERE s.request_id = r.id
        AND substr(s.session_id, instr(s.session_id, ':') + 1) LIKE sqlc.narg('session_prefix') || '%'
    )
  )
ORDER BY r.latency_ms;

-- name: ListRequestErrorKindsSince :many
SELECT COALESCE(r.error_kind, '') AS kind, COUNT(*) AS count
FROM requests r
WHERE r.ts >= sqlc.arg('ts')
  AND (sqlc.narg('key_id') IS NULL OR r.client_key_id = sqlc.narg('key_id'))
  AND (
    sqlc.narg('session_prefix') IS NULL
    OR EXISTS (
      SELECT 1
      FROM snapshots s
      WHERE s.request_id = r.id
        AND substr(s.session_id, instr(s.session_id, ':') + 1) LIKE sqlc.narg('session_prefix') || '%'
    )
  )
  AND (r.status_code >= 400 OR r.error_kind <> '')
GROUP BY COALESCE(r.error_kind, '')
ORDER BY count DESC, kind;

-- name: ListSuccessfulRequestLatenciesSince :many
SELECT r.latency_ms
FROM requests r
WHERE r.ts >= sqlc.arg('ts')
  AND (sqlc.narg('key_id') IS NULL OR r.client_key_id = sqlc.narg('key_id'))
  AND (
    sqlc.narg('session_prefix') IS NULL
    OR EXISTS (
      SELECT 1
      FROM snapshots s
      WHERE s.request_id = r.id
        AND substr(s.session_id, instr(s.session_id, ':') + 1) LIKE sqlc.narg('session_prefix') || '%'
    )
  )
  AND r.status_code < 400
  AND (r.error_kind IS NULL OR r.error_kind = '')
ORDER BY r.latency_ms;
