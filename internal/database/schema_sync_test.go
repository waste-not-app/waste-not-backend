package database

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestSchemaCopiesStayInSync guards the two copies of the core schema: the embedded runner file
// and the sqlc source under sql/migrations. Both must produce the same tables, columns, constraints and indexes.
func TestSchemaCopiesStayInSync(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	runner, err := migrations.ReadFile("001_core.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlc, err := os.ReadFile("../../sql/migrations/000001_init_core_schema.up.sql")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	describe := func(schema string, ddl []byte) []string {
		t.Helper()
		if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
			t.Fatal(err)
		}
		defer conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		if _, err := conn.Exec(ctx, "SET search_path TO "+schema); err != nil {
			t.Fatal(err)
		}
		defer conn.Exec(ctx, "SET search_path TO DEFAULT")
		if _, err := conn.Exec(ctx, string(ddl)); err != nil {
			t.Fatalf("%s: %v", schema, err)
		}
		var out []string
		collect := func(query string) {
			rows, err := conn.Query(ctx, query, schema)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					t.Fatal(err)
				}
				// schema_migrations is created by the runner itself (database.Migrate), not by 001_core.sql.
				if strings.Contains(line, "schema_migrations") {
					continue
				}
				out = append(out, strings.ReplaceAll(line, schema+".", ""))
			}
		}
		collect(`SELECT 'col ' || table_name || '.' || column_name || ' ' || data_type || ' ' || is_nullable || ' ' || coalesce(column_default, '')
			FROM information_schema.columns WHERE table_schema = $1 ORDER BY 1`)
		collect(`SELECT 'con ' || c.conrelid::regclass::text || ' ' || pg_get_constraintdef(c.oid)
			FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace WHERE n.nspname = $1 ORDER BY 1`)
		collect(`SELECT 'idx ' || indexdef FROM pg_indexes WHERE schemaname = $1 ORDER BY 1`)
		return out
	}

	a := describe("schema_sync_runner", runner)
	b := describe("schema_sync_sqlc", sqlc)
	if len(a) == 0 || !reflect.DeepEqual(a, b) {
		t.Fatalf("schema copies differ:\nrunner: %q\nsqlc:   %q", a, b)
	}
}
