-- Query set for trajectory capture (see design.md D1/D2/D6).
-- Session attribution lives in snapshots: a request without a snapshot joins no session.
-- Keep ASCII-only (see providers.sql note).

-- name: InsertRequestBody :exec
INSERT INTO request_bodies (
    request_id, method, path, query, body_gzip, headers
) VALUES (?, ?, ?, ?, ?, ?);

-- name: InsertSnapshot :exec
INSERT INTO snapshots (
    request_id, session_id, session_source, session_note, seq, ts,
    ndjson_gzip, line_hashes, line_sizes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: NextSnapshotSeq :one
SELECT COALESCE(MAX(seq), 0) + 1 FROM snapshots WHERE session_id = ?;

-- name: ListSnapshotsBySession :many
SELECT * FROM snapshots WHERE session_id = ? ORDER BY seq;

-- name: GetSnapshotByRequestID :one
SELECT * FROM snapshots WHERE request_id = ?;

-- name: GetRequestBodyByRequestID :one
SELECT * FROM request_bodies WHERE request_id = ?;

-- ListSessionSteps returns one session's steps joined with their request records:
-- the timeline skeleton needs snapshot columns (hashes/sizes for comparison) and
-- request columns (latency/status/usage) in a single ordered pass.
-- name: ListSessionSteps :many
SELECT
    s.request_id,
    s.seq,
    s.ts,
    s.session_id,
    s.session_source,
    s.session_note,
    s.ndjson_gzip,
    s.line_hashes,
    s.line_sizes,
    r.protocol,
    r.requested_model,
    r.upstream_model,
    r.status_code,
    r.latency_ms,
    r.stream,
    r.usage_in,
    r.usage_out,
    r.usage_cache,
    r.error_kind
FROM snapshots s
JOIN requests r ON r.id = s.request_id
WHERE s.session_id = ?
ORDER BY s.seq;

-- ListSessionSummaries returns the session list: steps equals the request count
-- (attribution lives in snapshots, so both are identical). Time filters select
-- sessions having any step inside the range. session_source and session_note are
-- representative values within a session; exact reasons live in the detail view.
-- name: ListSessionSummaries :many
SELECT
    s.session_id,
    CAST(MIN(s.session_source) AS TEXT) AS session_source,
    CAST(MIN(s.session_note) AS TEXT) AS session_note,
    CAST(MIN(s.ts) AS TEXT) AS first_seen,
    CAST(MAX(s.ts) AS TEXT) AS last_seen,
    COUNT(*) AS steps,
    CAST(COALESCE(SUM(r.usage_in), 0) AS INTEGER) AS usage_in,
    CAST(COALESCE(SUM(r.usage_out), 0) AS INTEGER) AS usage_out,
    CAST(COALESCE(SUM(r.usage_cache), 0) AS INTEGER) AS usage_cache
FROM snapshots s
JOIN requests r ON r.id = s.request_id
-- prefix matches the client identifier (the part after the colon), so search
-- follows what the UI shows.
WHERE (sqlc.narg('key_id') IS NULL OR s.session_id LIKE sqlc.narg('key_id') || ':%')
  AND (
    sqlc.narg('prefix') IS NULL
    OR substr(s.session_id, instr(s.session_id, ':') + 1) LIKE sqlc.narg('prefix') || '%'
  )
  AND (sqlc.narg('from_ts') IS NULL OR s.ts >= sqlc.narg('from_ts'))
  AND (sqlc.narg('to_ts') IS NULL OR s.ts <= sqlc.narg('to_ts'))
GROUP BY s.session_id
ORDER BY last_seen DESC
LIMIT sqlc.arg('row_limit');
