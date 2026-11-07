-- name: IsSchemaMigrated :one
SELECT EXISTS(
    SELECT 1 FROM schema_migrations WHERE name = '001_core.sql' OR name = '000001_init_core_schema.up.sql'
);
