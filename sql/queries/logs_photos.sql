-- name: GetLogWithPhotoByIDAnyState :one
SELECT id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, photo_id::text AS photo_id, version, created_at, updated_at
FROM waste_logs
WHERE id = $1 AND user_id = $2;

-- name: GetActiveLogWithPhotoByID :one
SELECT id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, photo_id::text AS photo_id, version, created_at, updated_at
FROM waste_logs
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: LockActiveLogWithPhotoByID :one
SELECT id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, photo_id::text AS photo_id, version, created_at, updated_at
FROM waste_logs
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
FOR UPDATE;

-- name: CreateWasteLogWithPhoto :one
INSERT INTO waste_logs (
    id, user_id, category, method, quantity_kind, weight_g, item_count, activity_date, activity_timezone, notes, photo_id, client_request_id, request_hash
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, photo_id::text AS photo_id, version, created_at, updated_at;

-- name: ListActiveLogsWithPhoto :many
SELECT id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, photo_id::text AS photo_id, version, created_at, updated_at
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

-- name: UpdateWasteLogWithPhoto :one
UPDATE waste_logs
SET category = $1, method = $2, quantity_kind = $3, weight_g = $4, item_count = $5, activity_date = $6, notes = $7, photo_id = $8, version = version + 1, updated_at = now()
WHERE id = $9 AND user_id = $10 AND version = $11 AND deleted_at IS NULL
RETURNING id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, photo_id::text AS photo_id, version, created_at, updated_at;

-- name: LockLogVersionAndPhotoForDelete :one
SELECT version, (deleted_at IS NOT NULL)::bool AS deleted, photo_id::text AS photo_id
FROM waste_logs
WHERE id = $1 AND user_id = $2
FOR UPDATE;

-- name: ListRecentActiveLogsWithPhoto :many
SELECT id::text AS id, category, method, quantity_kind, weight_g, item_count, activity_date::text AS activity_date, activity_timezone, notes, photo_id::text AS photo_id, version, created_at, updated_at
FROM waste_logs
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY activity_date DESC, id DESC
LIMIT 3;
