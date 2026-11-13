CREATE TABLE achievements (
    id uuid PRIMARY KEY,
    code text NOT NULL UNIQUE,
    title text NOT NULL,
    description text NOT NULL,
    requirement_type text NOT NULL CHECK (requirement_type IN ('ACTIVE_LOG_COUNT','RECYCLE_LOG_COUNT','BEST_STREAK')),
    requirement_value integer NOT NULL CHECK (requirement_value > 0),
    sort_order integer NOT NULL DEFAULT 0
);

CREATE TABLE user_achievements (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    achievement_id uuid NOT NULL REFERENCES achievements(id) ON DELETE CASCADE,
    unlocked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, achievement_id)
);

INSERT INTO achievements (id, code, title, description, requirement_type, requirement_value, sort_order) VALUES
    ('00000000-0000-4000-8000-000000000101', 'FIRST_STEP', 'Langkah pertama', 'Catat minimal 1 aktivitas pengelolaan sampah aktif.', 'ACTIVE_LOG_COUNT', 1, 1),
    ('00000000-0000-4000-8000-000000000102', 'GETTING_USED', 'Mulai terbiasa', 'Catat minimal 5 aktivitas pengelolaan sampah aktif.', 'ACTIVE_LOG_COUNT', 5, 2),
    ('00000000-0000-4000-8000-000000000103', 'SORT_TO_RECYCLE', 'Pilah untuk daur ulang', 'Catat minimal 3 aktivitas dengan metode Recycle.', 'RECYCLE_LOG_COUNT', 3, 3),
    ('00000000-0000-4000-8000-000000000104', 'CONSISTENT_WEEK', 'Satu minggu konsisten', 'Capai streak terbaik minimal 7 hari beruntun.', 'BEST_STREAK', 7, 4)
ON CONFLICT (code) DO NOTHING;

CREATE TABLE photos (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    object_key text NOT NULL UNIQUE,
    mime_type text NOT NULL CHECK (mime_type IN ('image/jpeg','image/png','image/webp')),
    size_bytes integer NOT NULL CHECK (size_bytes BETWEEN 1 AND 5242880),
    status text NOT NULL CHECK (status IN ('STAGED','ATTACHED','DELETED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX photos_user_status ON photos(user_id, status, created_at);

ALTER TABLE waste_logs ADD COLUMN photo_id uuid REFERENCES photos(id);
CREATE UNIQUE INDEX waste_logs_active_photo ON waste_logs(photo_id) WHERE photo_id IS NOT NULL AND deleted_at IS NULL;
