CREATE TABLE password_reset_tokens (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL
);
CREATE INDEX password_reset_tokens_expiry ON password_reset_tokens(expires_at);

CREATE TABLE password_reset_mail (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    version uuid NOT NULL DEFAULT gen_random_uuid(),
    encrypted_token bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    available_at timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5)
);
CREATE INDEX password_reset_mail_pending ON password_reset_mail(available_at) WHERE attempts < 5;

CREATE TABLE photo_cleanup_jobs (
    object_key text PRIMARY KEY,
    queued_at timestamptz NOT NULL DEFAULT now(),
    available_at timestamptz NOT NULL DEFAULT now(),
    attempts integer NOT NULL DEFAULT 0,
    uploading boolean NOT NULL DEFAULT false
);
CREATE INDEX photo_cleanup_jobs_ready ON photo_cleanup_jobs(available_at, queued_at);

CREATE FUNCTION queue_deleted_photo() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO photo_cleanup_jobs(object_key)
    VALUES (OLD.object_key) ON CONFLICT DO NOTHING;
    RETURN OLD;
END;
$$;
CREATE TRIGGER photos_cleanup_after_delete AFTER DELETE ON photos
    FOR EACH ROW EXECUTE FUNCTION queue_deleted_photo();

CREATE FUNCTION queue_retired_photo() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'DELETED' AND OLD.status <> 'DELETED' THEN
        INSERT INTO photo_cleanup_jobs(object_key)
        VALUES (NEW.object_key) ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER photos_cleanup_after_retirement AFTER UPDATE OF status ON photos
    FOR EACH ROW EXECUTE FUNCTION queue_retired_photo();

INSERT INTO photo_cleanup_jobs(object_key)
SELECT object_key FROM photos WHERE status = 'DELETED';
