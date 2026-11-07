-- name: AcquireLogIdempotencyLock :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key)::text, 0));

-- name: GetLogByClientRequestID :one
SELECT id::text AS id, request_hash, (deleted_at IS NOT NULL)::bool AS deleted
FROM waste_logs
WHERE user_id = $1 AND client_request_id = $2;

-- name: GetLogByIDAnyState :one
SELECT id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, version, created_at, updated_at
FROM waste_logs
WHERE id = $1 AND user_id = $2;

-- name: GetActiveLogByID :one
SELECT id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, version, created_at, updated_at
FROM waste_logs
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: LockActiveLogByID :one
SELECT id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, version, created_at, updated_at
FROM waste_logs
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
FOR UPDATE;

-- name: CreateWasteLog :one
INSERT INTO waste_logs (
    id, user_id, category, method, quantity_kind, weight_g, item_count, activity_date, activity_timezone, notes, client_request_id, request_hash
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, version, created_at, updated_at;

-- name: ListActiveLogs :many
SELECT id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, version, created_at, updated_at
FROM waste_logs
WHERE user_id = sqlc.arg(user_id)
  AND deleted_at IS NULL
  AND (sqlc.arg(category)::text = '' OR category = sqlc.arg(category)::text)
  AND (sqlc.arg(method)::text = '' OR method = sqlc.arg(method)::text)
  AND (sqlc.arg(from_date)::text = '' OR activity_date >= NULLIF(sqlc.arg(from_date)::text, '')::date)
  AND (sqlc.arg(to_date)::text = '' OR activity_date <= NULLIF(sqlc.arg(to_date)::text, '')::date)
  AND (sqlc.arg(date_pos)::text = '' OR (activity_date, id) < (NULLIF(sqlc.arg(date_pos)::text, '')::date, NULLIF(sqlc.arg(id_pos)::text, '')::uuid))
ORDER BY activity_date DESC, id DESC
LIMIT sqlc.arg(page_limit)::int;

-- name: UpdateWasteLog :one
UPDATE waste_logs
SET category = $1, method = $2, quantity_kind = $3, weight_g = $4, item_count = $5, activity_date = $6, notes = $7, version = version + 1, updated_at = now()
WHERE id = $8 AND user_id = $9 AND version = $10 AND deleted_at IS NULL
RETURNING id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, version, created_at, updated_at;

-- name: LockLogVersionForDelete :one
SELECT version, (deleted_at IS NOT NULL)::bool AS deleted
FROM waste_logs
WHERE id = $1 AND user_id = $2
FOR UPDATE;

-- name: SoftDeleteWasteLog :exec
UPDATE waste_logs
SET deleted_at = now(), updated_at = now(), version = version + 1
WHERE id = $1 AND user_id = $2 AND version = $3;

-- name: LockRecommendationForProvenance :one
SELECT state, response, expires_at, (deleted_at IS NOT NULL)::bool AS deleted
FROM ai_operations
WHERE id = $1 AND user_id = $2 AND operation = 'RECOMMENDATION'
FOR UPDATE;

-- name: IsRecommendationLinked :one
SELECT EXISTS(SELECT 1 FROM ai_log_metadata WHERE recommendation_id = $1);

-- name: LinkAIProvenance :exec
INSERT INTO ai_log_metadata (waste_log_id, recommendation_id, user_id, snapshot, final_category, final_method, accepted)
SELECT sqlc.arg(waste_log_id), id, sqlc.arg(user_id), snapshot, sqlc.arg(final_category), sqlc.arg(final_method), response->>'recommended_method' = sqlc.arg(final_method)
FROM ai_operations
WHERE id = sqlc.arg(recommendation_id) AND user_id = sqlc.arg(user_id);
