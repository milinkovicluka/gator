-- +goose Up
ALTER TABLE users ADD COLUMN api_key_hash TEXT UNIQUE;

-- +goose Down
ALTER TABLE users DROP COLUMN api_key_hash;