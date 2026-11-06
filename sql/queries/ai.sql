-- name: LockDetectionOperation :one
SELECT id::text AS id, state, request_hash, response, snapshot, COALESCE(http_status, 0)::int AS http_status, COALESCE(error_code, '')::text AS error_code, COALESCE(retry_after, 0)::int AS retry_after, expires_at, (deleted_at IS NOT NULL)::bool AS deleted
FROM ai_operations
WHERE id = $1 AND user_id = $2 AND operation = 'DETECTION'
FOR UPDATE;

-- name: InsertAIOperationIdempotent :execrows
INSERT INTO ai_operations (id, user_id, operation, client_request_id, request_hash, detection_id, state, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, 'IN_PROGRESS', $7)
ON CONFLICT (user_id, operation, client_request_id) DO NOTHING;

-- name: GetAIOperationByClientRequestID :one
SELECT id::text AS id, state, request_hash, response, snapshot, COALESCE(http_status, 0)::int AS http_status, COALESCE(error_code, '')::text AS error_code, COALESCE(retry_after, 0)::int AS retry_after, expires_at, (deleted_at IS NOT NULL)::bool AS deleted
FROM ai_operations
WHERE user_id = $1 AND operation = $2 AND client_request_id = $3;

-- name: GetAIOperationByID :one
SELECT id::text AS id, state, request_hash, response, snapshot, COALESCE(http_status, 0)::int AS http_status, COALESCE(error_code, '')::text AS error_code, COALESCE(retry_after, 0)::int AS retry_after, expires_at, (deleted_at IS NOT NULL)::bool AS deleted
FROM ai_operations
WHERE id = $1 AND user_id = $2;

-- name: GetAIOperationByIDAndKind :one
SELECT id::text AS id, state, request_hash, response, snapshot, COALESCE(http_status, 0)::int AS http_status, COALESCE(error_code, '')::text AS error_code, COALESCE(retry_after, 0)::int AS retry_after, expires_at, (deleted_at IS NOT NULL)::bool AS deleted
FROM ai_operations
WHERE id = $1 AND user_id = $2 AND operation = $3;

-- name: CompleteAIOperation :execrows
UPDATE ai_operations o
SET state = $1, response = $2, snapshot = $3, http_status = $4, error_code = $5, retry_after = $6
WHERE o.id = $7
  AND o.user_id = $8
  AND o.state = 'IN_PROGRESS'
  AND o.deleted_at IS NULL
  AND o.expires_at > now()
  AND (
      o.detection_id IS NULL
      OR EXISTS (
          SELECT 1 FROM ai_operations d
          WHERE d.id = o.detection_id
            AND d.user_id = o.user_id
            AND d.deleted_at IS NULL
            AND d.expires_at > now()
      )
  );

-- name: LockDetectionIDForDelete :one
SELECT id::text AS id
FROM ai_operations
WHERE id = $1 AND user_id = $2 AND operation = 'DETECTION'
FOR UPDATE;

-- name: SoftDeleteDetectionCascade :exec
UPDATE ai_operations
SET deleted_at = COALESCE(deleted_at, now()), response = NULL, snapshot = NULL
WHERE user_id = $1 AND (id = $2 OR detection_id = $2);
