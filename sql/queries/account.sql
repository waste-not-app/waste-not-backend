-- name: LockAccount :one
SELECT password_hash FROM users WHERE id = $1 FOR UPDATE;

-- name: LockAccountByEmail :one
SELECT id::text AS id FROM users WHERE normalized_email = $1 FOR UPDATE;

-- name: ReplacePasswordResetToken :exec
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at;

-- name: GetPasswordResetOwner :one
SELECT user_id::text AS user_id FROM password_reset_tokens WHERE token_hash = $1;

-- name: LockPasswordResetToken :one
SELECT expires_at FROM password_reset_tokens WHERE user_id = $1 AND token_hash = $2 FOR UPDATE;

-- name: ReplaceAccountPassword :exec
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;

-- name: DeletePasswordResetTokens :exec
DELETE FROM password_reset_tokens WHERE user_id = $1;

-- name: DeleteAccountSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteAccount :execrows
DELETE FROM users WHERE id = $1;

-- name: QueuePasswordResetMail :exec
INSERT INTO password_reset_mail (user_id, encrypted_token, expires_at, available_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (user_id) DO UPDATE
SET encrypted_token = EXCLUDED.encrypted_token, expires_at = EXCLUDED.expires_at,
    available_at = EXCLUDED.available_at, attempts = 0, version = gen_random_uuid();

-- name: ClaimPasswordResetMail :one
WITH candidate AS (
    SELECT user_id FROM password_reset_mail
    WHERE available_at <= now() AND expires_at > now() AND attempts < 5
    ORDER BY available_at, user_id LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE password_reset_mail m
SET available_at = now() + interval '30 seconds', attempts = m.attempts + 1
FROM candidate c, users u
WHERE m.user_id = c.user_id AND u.id = m.user_id
RETURNING m.user_id::text AS user_id, m.version::text AS version,
    u.normalized_email, m.encrypted_token, m.attempts;

-- name: DeletePasswordResetMail :exec
DELETE FROM password_reset_mail WHERE user_id = $1;

-- name: AcknowledgePasswordResetMail :exec
DELETE FROM password_reset_mail WHERE user_id = $1 AND version = $2;

-- name: DeferPasswordResetMail :exec
UPDATE password_reset_mail
SET available_at = now() + sqlc.arg(delay_seconds)::int * interval '1 second'
WHERE user_id = sqlc.arg(user_id) AND version = sqlc.arg(version);

-- name: LockPhotoCleanupJob :one
SELECT object_key, attempts FROM photo_cleanup_jobs
WHERE (sqlc.arg(owner_prefix)::text = '' OR starts_with(object_key, sqlc.arg(owner_prefix)::text))
    AND (available_at <= now() OR (sqlc.arg(owner_prefix)::text <> '' AND uploading))
ORDER BY queued_at, object_key LIMIT 1 FOR UPDATE SKIP LOCKED;

-- name: DeletePhotoCleanupJob :exec
DELETE FROM photo_cleanup_jobs WHERE object_key = $1;

-- name: QueuePhotoUploadIntent :exec
INSERT INTO photo_cleanup_jobs(object_key, uploading, available_at)
VALUES ($1, true, now() + interval '2 minutes');

-- name: LockPhotoUploadIntent :one
SELECT object_key FROM photo_cleanup_jobs WHERE object_key = $1 AND uploading FOR UPDATE;

-- name: DeferPhotoCleanupJob :exec
UPDATE photo_cleanup_jobs
SET attempts = attempts + 1, uploading = false,
    available_at = now() + sqlc.arg(delay_seconds)::int * interval '1 second'
WHERE object_key = sqlc.arg(object_key);

-- name: HasOwnerPhotoCleanup :one
SELECT EXISTS(SELECT 1 FROM photo_cleanup_jobs WHERE starts_with(object_key, $1::text));
