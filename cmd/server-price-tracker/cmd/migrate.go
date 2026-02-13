package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/donaldgifford/server-price-tracker/internal/config"
	"github.com/donaldgifford/server-price-tracker/internal/store"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	RunE:  runMigrate,
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}

func runMigrate(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := log.NewWithOptions(os.Stderr, log.Options{
		Level: parseLogLevel(cfg.Logging.Level),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.Database.DSN())
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	logger.Info("running migrations", "dsn", cfg.Database.Host)

	if err := store.RunMigrations(ctx, pool); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	logger.Info("migrations complete")
	return nil
}
