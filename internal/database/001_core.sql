CREATE TABLE users (
 id uuid PRIMARY KEY, name varchar(60) NOT NULL CHECK (char_length(name) BETWEEN 2 AND 60),
 normalized_email varchar(254) NOT NULL UNIQUE, password_hash text NOT NULL,
 timezone text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE sessions (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 generation integer NOT NULL DEFAULT 1, expires_at timestamptz NOT NULL,
 revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE TABLE refresh_tokens (
 token_hash text PRIMARY KEY, session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 used_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE waste_logs (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 category text NOT NULL CHECK(category IN ('ORGANIC','PLASTIC','PAPER','GLASS','METAL','ELECTRONIC','OTHER')),
 method text NOT NULL CHECK(method IN ('REDUCE','REUSE','RECYCLE','TRASH')),
 quantity_kind text NOT NULL CHECK(quantity_kind IN ('WEIGHT','COUNT')),
 weight_g integer, item_count integer, activity_date date NOT NULL, activity_timezone text NOT NULL,
 notes varchar(500) NOT NULL DEFAULT '', client_request_id uuid NOT NULL, request_hash text NOT NULL,
 version integer NOT NULL DEFAULT 1 CHECK(version>=1),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz,
 UNIQUE(user_id, client_request_id), UNIQUE(user_id,id),
 CHECK ((quantity_kind='WEIGHT' AND weight_g IS NOT NULL AND weight_g BETWEEN 1 AND 100000 AND item_count IS NULL)
 OR (quantity_kind='COUNT' AND item_count IS NOT NULL AND item_count BETWEEN 1 AND 10000 AND weight_g IS NULL))
);
CREATE INDEX waste_logs_active ON waste_logs(user_id,activity_date DESC,id DESC) WHERE deleted_at IS NULL;
CREATE TABLE waste_guides (
 id uuid PRIMARY KEY, slug text NOT NULL UNIQUE, name text NOT NULL, category text NOT NULL,
 keywords text[] NOT NULL DEFAULT '{}', recommended_method text NOT NULL,
 summary text NOT NULL, steps jsonb NOT NULL, cautions jsonb NOT NULL,
 source_url text NOT NULL, reviewed_at date NOT NULL, reviewer text NOT NULL,
 publication_status text NOT NULL CHECK(publication_status IN ('DRAFT','PUBLISHED','ARCHIVED')),
 CHECK(category IN ('ORGANIC','PLASTIC','PAPER','GLASS','METAL','ELECTRONIC','OTHER')),
 CHECK(recommended_method IN ('REDUCE','REUSE','RECYCLE','TRASH')),
 CHECK(jsonb_typeof(steps)='array' AND jsonb_typeof(cautions)='array')
);
CREATE TABLE rate_limits (
 key text NOT NULL, bucket timestamptz NOT NULL, hits integer NOT NULL CHECK(hits>0),
 PRIMARY KEY(key,bucket)
);
CREATE INDEX rate_limits_expiry ON rate_limits(bucket);
CREATE TABLE ai_operations (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 operation text NOT NULL CHECK(operation IN ('DETECTION','RECOMMENDATION')),
 client_request_id uuid NOT NULL, request_hash text NOT NULL,
 detection_id uuid REFERENCES ai_operations(id) ON DELETE CASCADE,
 state text NOT NULL CHECK(state IN ('IN_PROGRESS','COMPLETED','FAILED_UNCERTAIN')),
 response jsonb, http_status integer, error_code text, retry_after integer,
 snapshot jsonb, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz,
 UNIQUE(user_id,operation,client_request_id), UNIQUE(user_id,id)
);
CREATE INDEX ai_operations_expiry ON ai_operations(expires_at);
CREATE TABLE ai_log_metadata (
 waste_log_id uuid PRIMARY KEY, recommendation_id uuid NOT NULL UNIQUE, user_id uuid NOT NULL,
 snapshot jsonb NOT NULL, final_category text NOT NULL, final_method text NOT NULL, accepted boolean NOT NULL,
 FOREIGN KEY(user_id,waste_log_id) REFERENCES waste_logs(user_id,id) ON DELETE CASCADE
);
