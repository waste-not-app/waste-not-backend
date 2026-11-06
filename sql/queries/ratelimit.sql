-- name: IncrementRateLimit :one
INSERT INTO rate_limits (key, bucket, hits)
VALUES (sqlc.arg(key), sqlc.arg(bucket), 1)
ON CONFLICT (key, bucket) DO UPDATE
SET hits = LEAST(rate_limits.hits + 1, sqlc.arg(max_hits)::int + 1)
RETURNING hits;
