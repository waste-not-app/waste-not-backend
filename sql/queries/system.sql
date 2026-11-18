-- name: IsSchemaMigrated :one
SELECT EXISTS(
    SELECT 1 FROM schema_migrations WHERE name = $1
);
