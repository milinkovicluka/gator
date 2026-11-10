-- name: CreatePost :one
INSERT INTO posts (id, created_at, updated_at, title, url, description, published_at, feed_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetPostsForUser :many
SELECT posts.*, feeds.name AS feed_name
FROM posts
INNER JOIN feed_follows ON feed_follows.feed_id = posts.feed_id
INNER JOIN feeds ON feeds.id = posts.feed_id
WHERE feed_follows.user_id = sqlc.arg(user_id)
  AND feeds.name ILIKE '%' || sqlc.arg(feed_name)::text || '%'
ORDER BY
  CASE WHEN sqlc.arg(sort_order)::text = 'oldest' THEN posts.published_at END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort_order)::text <> 'oldest' THEN posts.published_at END DESC NULLS LAST
LIMIT sqlc.arg('limit')
OFFSET sqlc.arg('offset');


-- name: SearchPostsForUser :many
SELECT posts.*, feeds.name AS feed_name
FROM posts
INNER JOIN feed_follows ON feed_follows.feed_id = posts.feed_id
INNER JOIN feeds ON feeds.id = posts.feed_id
WHERE feed_follows.user_id = sqlc.arg(user_id)
  AND (
    posts.title ILIKE '%' || sqlc.arg(query)::text || '%'
    OR posts.description ILIKE '%' || sqlc.arg(query)::text || '%'
    OR word_similarity(sqlc.arg(query)::text, posts.title) >= 0.5
  )
ORDER BY
  (posts.title ILIKE '%' || sqlc.arg(query)::text || '%') DESC,
  word_similarity(sqlc.arg(query)::text, posts.title) DESC,
  posts.published_at DESC NULLS LAST
LIMIT sqlc.arg('limit')
OFFSET sqlc.arg('offset');

-- name: GetPostByURL :one
SELECT * FROM posts
WHERE url = $1;