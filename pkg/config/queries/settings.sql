-- Query set for settings (key-value store). Keep ASCII-only (see providers.sql note).

-- name: ListSettings :many
SELECT * FROM settings ORDER BY key;

-- name: GetSetting :one
SELECT * FROM settings WHERE key = ?;

-- name: UpsertSetting :exec
INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?);

-- name: DeleteSetting :exec
DELETE FROM settings WHERE key = ?;
