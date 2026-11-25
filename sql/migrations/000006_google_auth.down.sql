DO $$
BEGIN
    LOCK TABLE users IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM users WHERE google_sub IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove Google authentication while linked accounts exist';
    END IF;
    ALTER TABLE users
        ALTER COLUMN password_hash SET NOT NULL,
        DROP CONSTRAINT users_has_credential,
        DROP COLUMN google_sub;
    DROP TABLE google_auth_challenges;
END $$;
