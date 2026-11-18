package database

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var migrations embed.FS

func LatestMigration() string {
	files, _ := migrations.ReadDir(".")
	latest := ""
	for _, f := range files {
		if name := f.Name(); name > latest {
			latest = name
		}
	}
	return latest
}

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	c, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid database configuration")
	}
	c.MaxConns = 10
	c.MinConns = 1
	c.MaxConnLifetime = 30 * time.Minute
	c.ConnConfig.RuntimeParams["timezone"] = "UTC"
	c.ConnConfig.RuntimeParams["statement_timeout"] = "25000"
	c.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "45000"
	p, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("database unavailable")
	}
	return p, nil
}

func Migrate(ctx context.Context, p *pgxpool.Pool) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(724901)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(name text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	files, _ := migrations.ReadDir(".")
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, f := range files {
		raw, _ := migrations.ReadFile(f.Name())
		sum := fmt.Sprintf("%x", sha256.Sum256(raw))
		var stored string
		err = tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE name=$1`, f.Name()).Scan(&stored)
		if err == nil {
			if stored != sum {
				return fmt.Errorf("migration checksum changed: %s", f.Name())
			}
			continue
		}
		if err != pgx.ErrNoRows {
			return err
		}
		if _, err = tx.Exec(ctx, string(raw)); err != nil {
			return fmt.Errorf("migration %s: %w", f.Name(), err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)`, f.Name(), sum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func Cleanup(ctx context.Context, p *pgxpool.Pool) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, q := range []string{
		`UPDATE ai_operations SET state='FAILED_UNCERTAIN',error_code='AI_TIMEOUT',http_status=504 WHERE state='IN_PROGRESS' AND created_at<now()-interval '2 minutes'`,
		`DELETE FROM ai_operations WHERE expires_at<now()`,
		`UPDATE photos SET status='DELETED',deleted_at=COALESCE(deleted_at,now()) WHERE status='STAGED' AND deleted_at IS NULL AND created_at<now()-interval '24 hours'`,
		`DELETE FROM rate_limits WHERE bucket<now()-interval '2 days'`,
		`DELETE FROM password_reset_tokens WHERE expires_at<now()`,
		`DELETE FROM password_reset_mail WHERE expires_at<now()`,
		`DELETE FROM sessions WHERE expires_at<now()-interval '7 days'`,
	} {
		if _, err = tx.Exec(ctx, q); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
