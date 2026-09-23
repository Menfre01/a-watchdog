-- Starter query set for providers (remaining entities arrive in task 3.2).
-- NOTE: keep this file ASCII-only -- sqlc's SQLite query editor corrupts
-- statements when multibyte (e.g. CJK) characters appear before placeholders.

-- name: ListProviders :many
SELECT * FROM providers ORDER BY id;

-- name: GetProvider :one
SELECT * FROM providers WHERE id = ?;

-- name: CreateProvider :execresult
INSERT INTO providers (name, protocol, base_url, api_key, enabled, probe_model)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateProvider :exec
UPDATE providers
SET name = ?, protocol = ?, base_url = ?, api_key = ?, enabled = ?, probe_model = ?
WHERE id = ?;

-- name: DeleteProvider :exec
DELETE FROM providers WHERE id = ?;
