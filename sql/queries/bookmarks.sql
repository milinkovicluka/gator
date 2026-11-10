-- name: CreateBookmark :execrows
INSERT INTO bookmarks (id, created_at, updated_at, user_id, post_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, post_id) DO NOTHING;

-- name: DeleteBookmark :execrows
DELETE FROM bookmarks
WHERE user_id = $1 AND post_id = $2;

-- name: GetBookmarksForUser :many
SELECT posts.*, feeds.name AS feed_name
FROM bookmarks
INNER JOIN posts ON posts.id = bookmarks.post_id
INNER JOIN feeds ON feeds.id = posts.feed_id
WHERE bookmarks.user_id = sqlc.arg(user_id)
ORDER BY bookmarks.created_at DESC
LIMIT sqlc.arg('limit')
OFFSET sqlc.arg('offset');