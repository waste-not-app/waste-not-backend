ALTER TABLE users
    ADD COLUMN google_sub text UNIQUE CHECK (char_length(google_sub) BETWEEN 1 AND 255),
    ALTER COLUMN password_hash DROP NOT NULL,
    ADD CONSTRAINT users_has_credential CHECK (password_hash IS NOT NULL OR google_sub IS NOT NULL);

CREATE TABLE google_auth_challenges (
    nonce_hash text PRIMARY KEY,
    expires_at timestamptz NOT NULL
);
CREATE INDEX google_auth_challenges_expiry ON google_auth_challenges(expires_at);
