-- Query set for models (model catalog). Keep ASCII-only (see providers.sql note).

-- name: ListModels :many
SELECT * FROM models ORDER BY id;

-- name: GetModel :one
SELECT * FROM models WHERE id = ?;

-- name: GetModelByName :one
SELECT * FROM models WHERE name = ?;

-- name: CreateModel :execresult
INSERT INTO models (name, provider_id, upstream_model, enabled)
VALUES (?, ?, ?, ?);

-- name: UpdateModel :exec
UPDATE models
SET name = ?, provider_id = ?, upstream_model = ?, enabled = ?
WHERE id = ?;

-- name: DeleteModel :exec
DELETE FROM models WHERE id = ?;
