-- name: CreatePhoto :one
INSERT INTO photos (id, user_id, object_key, mime_type, size_bytes, status)
VALUES ($1, $2, $3, $4, $5, 'STAGED')
RETURNING id::text AS id, user_id::text AS user_id, object_key, mime_type, size_bytes, status, created_at;

-- name: GetActivePhotoByID :one
SELECT id::text AS id, user_id::text AS user_id, object_key, mime_type, size_bytes, status, created_at
FROM photos
WHERE id = $1 AND user_id = $2 AND status <> 'DELETED' AND deleted_at IS NULL;

-- name: LockPhotoForUpdate :one
SELECT id::text AS id, user_id::text AS user_id, object_key, mime_type, size_bytes, status, (deleted_at IS NOT NULL)::bool AS deleted
FROM photos
WHERE id = $1 AND user_id = $2
FOR UPDATE;

-- name: MarkPhotoAttached :exec
UPDATE photos
SET status = 'ATTACHED'
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: MarkPhotoDeleted :exec
UPDATE photos
SET status = 'DELETED', deleted_at = COALESCE(deleted_at, now())
WHERE id = $1 AND user_id = $2;

-- name: IsPhotoAttachedToOtherActiveLog :one
SELECT EXISTS(
    SELECT 1
    FROM waste_logs
    WHERE photo_id = $1 AND deleted_at IS NULL AND id <> $2
);
