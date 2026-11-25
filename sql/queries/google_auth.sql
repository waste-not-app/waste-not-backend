-- name: IssueGoogleChallenge :exec
INSERT INTO google_auth_challenges (nonce_hash, expires_at)
VALUES ($1, clock_timestamp() + interval '5 minutes');

-- name: ClaimGoogleChallenge :execrows
DELETE FROM google_auth_challenges WHERE nonce_hash = $1 AND expires_at > clock_timestamp();

-- name: LockGoogleSubject :exec
SELECT pg_advisory_xact_lock(hashtextextended('auth-google:' || sqlc.arg(subject)::text, 0));

-- name: FindGoogleAccount :one
SELECT id::text AS id FROM users
WHERE google_sub = sqlc.arg(subject)::text OR normalized_email = sqlc.arg(email)::text
ORDER BY (google_sub = sqlc.arg(subject)::text) DESC NULLS LAST
LIMIT 1;

-- name: CreateGoogleUser :execrows
INSERT INTO users (id, name, normalized_email, timezone, google_sub)
VALUES ($1, $2, $3, $4, sqlc.arg(subject)::text)
ON CONFLICT (normalized_email) DO NOTHING;

-- name: LinkGoogleSubject :exec
UPDATE users SET google_sub = sqlc.arg(subject)::text, updated_at = now()
WHERE id = $1 AND google_sub IS NULL;
