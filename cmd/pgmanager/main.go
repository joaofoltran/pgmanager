package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmanager/internal/appconfig"
	"github.com/jfoltran/pgmanager/internal/backup"
	"github.com/jfoltran/pgmanager/internal/cluster"
	"github.com/jfoltran/pgmanager/internal/db"
	"github.com/jfoltran/pgmanager/internal/migrationstore"
	"github.com/jfoltran/pgmanager/internal/monitoring"
	"github.com/jfoltran/pgmanager/internal/secret"
	"github.com/jfoltran/pgmanager/internal/server"
)

var (
	configPath string
	debugMode  bool
)

var rootCmd = &cobra.Command{
	Use:   "pgmanager",
	Short: "PostgreSQL administration suite",
	Long: `pgmanager is a centralized PostgreSQL administration platform.
It provides a web UI for managing clusters, running migrations,
backups, and monitoring.

Configuration is loaded from (in order):
  1. --config flag
  2. ~/.pgmanager/config.toml
  3. /etc/pgmanager/config.toml
  4. Environment variables (PGMANAGER_DB_URL, PGMANAGER_PORT, etc.)
  5. Built-in defaults`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd.Context())
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to config file")
	rootCmd.PersistentFlags().BoolVar(&debugMode, "debug", false, "Enable debug logging")
	rootCmd.AddCommand(migrateCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(parentCtx context.Context) error {
	cfg, err := appconfig.Load(configPath)
	if err != nil {
		return err
	}

	var logger zerolog.Logger
	switch cfg.Logging.Format {
	case "json":
		logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	default:
		logger = zerolog.New(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		}).With().Timestamp().Logger()
	}

	level, err := zerolog.ParseLevel(cfg.Logging.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	if debugMode {
		level = zerolog.DebugLevel
	}
	logger = logger.Level(level)

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	logger.Info().Str("db", redactURL(cfg.Database.URL)).Msg("connecting to backend database")

	database, err := db.Open(ctx, cfg.Database.URL, logger)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer database.Close()

	var cipher *secret.Box
	if cfg.Security.EncryptionKey != "" {
		cipher, err = secret.NewBox(cfg.Security.EncryptionKey)
		if err != nil {
			return fmt.Errorf("encryption setup: %w", err)
		}
		logger.Info().Msg("password encryption enabled")
	} else {
		logger.Warn().Msg("PGMANAGER_ENCRYPTION_KEY not set — node passwords stored in plaintext")
	}

	clusters := cluster.NewStore(database.Pool, cipher)
	if cipher != nil {
		rotated, err := clusters.EncryptExistingPasswords(ctx)
		if err != nil {
			return fmt.Errorf("encrypt existing passwords: %w", err)
		}
		if rotated > 0 {
			logger.Info().Int("count", rotated).Msg("encrypted plaintext passwords")
		}
	}
	migrations := migrationstore.NewStore(database.Pool)
	backups := backup.NewStore(database.Pool)
	runner := migrationstore.NewRunner(ctx, migrations, clusters, logger)
	if err := runner.RecoverStale(ctx); err != nil {
		logger.Warn().Err(err).Msg("failed to recover stale migrations")
	}

	monStore := monitoring.NewStore(database.Pool, logger)
	partMgr := monitoring.NewPartitionManager(database.Pool, logger)
	monStore.SetPartitionManager(partMgr)
	partMgr.StartMaintainer(ctx)
	mon := monitoring.NewCollector(clusters, monStore, logger, monitoring.DefaultTierConfig())
	mon.SetPartitionManager(partMgr)
	defer mon.Close()
	if err := mon.AutoStart(ctx); err != nil {
		logger.Warn().Err(err).Msg("monitoring auto-start failed")
	}

	srv := server.New(nil, nil, logger)
	srv.SetClusterStore(clusters)
	srv.SetMigrationStore(migrations, runner)
	srv.SetBackupStore(backups)
	srv.SetMonitoringCollector(mon)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		logger.Info().Str("signal", sig.String()).Msg("shutting down")
		cancel()
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Listen, cfg.Server.Port)
	logger.Info().Str("addr", addr).Msg("starting pgmanager")

	return srv.Start(ctx, cfg.Server.Listen, cfg.Server.Port)
}

func redactURL(u string) string {
	for i, c := range u {
		if c == '@' {
			for j := i - 1; j >= 0; j-- {
				if u[j] == ':' && j > 10 {
					return u[:j+1] + "***" + u[i:]
				}
			}
		}
	}
	return u
}

func silentLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}
