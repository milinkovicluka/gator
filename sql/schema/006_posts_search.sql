-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX posts_title_trgm_idx ON posts USING GIN (title gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS posts_title_trgm_idx;