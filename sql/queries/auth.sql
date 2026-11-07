-- name: CreateUser :exec
INSERT INTO users (id, name, normalized_email, password_hash, timezone)
VALUES ($1, $2, $3, $4, $5);

-- name: GetUserByEmail :one
SELECT id::text AS id, name, normalized_email, timezone, password_hash
FROM users
WHERE normalized_email = $1;

-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, expires_at)
VALUES ($1, $2, $3);

-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (token_hash, session_id)
VALUES ($1, $2);

-- name: GetSessionAndUser :one
SELECT u.id::text AS id, u.name, u.normalized_email, u.timezone, s.generation, (s.revoked_at IS NOT NULL)::bool AS revoked, s.expires_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.id = $1 AND s.user_id = $2;

-- name: LockRefreshTokenSession :one
SELECT s.id::text AS session_id, s.user_id::text AS user_id, s.generation, s.expires_at, (t.used_at IS NOT NULL)::bool AS used, (s.revoked_at IS NOT NULL)::bool AS revoked
FROM refresh_tokens t
JOIN sessions s ON s.id = t.session_id
WHERE t.token_hash = $1
FOR UPDATE OF s, t;

-- name: MarkRefreshTokenUsed :exec
UPDATE refresh_tokens
SET used_at = now()
WHERE token_hash = $1;

-- name: IncrementSessionGeneration :exec
UPDATE sessions
SET generation = generation + 1
WHERE id = $1;

-- name: RevokeSessionByID :exec
UPDATE sessions
SET revoked_at = COALESCE(revoked_at, now())
WHERE id = $1;

-- name: RevokeUserSession :exec
UPDATE sessions
SET revoked_at = COALESCE(revoked_at, now())
WHERE id = $1 AND user_id = $2;

-- name: GetUserProgressStats :one
SELECT count(*)::bigint AS count, COALESCE(array_agg(DISTINCT activity_date::text ORDER BY activity_date::text), ARRAY[]::text[])::text[] AS activity_dates
FROM waste_logs
WHERE user_id = $1 AND deleted_at IS NULL;
