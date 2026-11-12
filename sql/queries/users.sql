-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, name)
VALUES (
    $1,
    $2,
    $3,
    $4
)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users
WHERE name = $1;

-- name: DeleteUsers :exec
DELETE FROM users;

-- name: GetUsers :many
SELECT * FROM users;


-- name: SetUserAPIKeyHash :exec
UPDATE users
SET api_key_hash = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: GetUserByAPIKeyHash :one
SELECT * FROM users
WHERE api_key_hash = $1;