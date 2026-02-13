package store

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations applies pending SQL migrations in order.
// Migrations are tracked in a schema_migrations table.
// There are no down migrations; fix forward only.
//
// TODO(test): RunMigrations requires a live Postgres instance, tested via integration tests only.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Ensure the schema_migrations table exists.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	// Read all migration files.
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations directory: %w", err)
	}

	// Sort by filename (lexicographic order gives us version order).
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := entry.Name()

		// Check if already applied.
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)",
			version,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if exists {
			continue
		}

		// Read and execute the migration.
		sql, err := migrationsFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", version, err)
		}

		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("applying migration %s: %w", version, err)
		}

		// Record the migration.
		if _, err := pool.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)",
			version,
		); err != nil {
			return fmt.Errorf("recording migration %s: %w", version, err)
		}
	}

	return nil
}
