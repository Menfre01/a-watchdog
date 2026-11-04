-- Query set for client_keys. Keep ASCII-only (see providers.sql note).

-- name: ListClientKeys :many
SELECT * FROM client_keys ORDER BY id;

-- name: GetClientKeyByHash :one
SELECT * FROM client_keys WHERE key_hash = ?;

-- name: CreateClientKey :execresult
INSERT INTO client_keys (name, key_hash, key_prefix, enabled)
VALUES (?, ?, ?, ?);

-- name: SetClientKeyEnabled :exec
UPDATE client_keys SET enabled = ? WHERE id = ?;

-- name: DeleteClientKey :execresult
DELETE FROM client_keys WHERE id = ?;
