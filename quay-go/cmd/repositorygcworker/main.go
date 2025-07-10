package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/quay/quay-go/internal/config"
	"github.com/quay/quay-go/internal/database"
	"github.com/quay/quay-go/internal/metrics"
	"github.com/quay/quay-go/internal/queue"
	"github.com/quay/quay-go/internal/repositorygc"
)

var (
	configPath string
	version    = "dev"
	commit     = "unknown"
	buildDate  = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "repositorygcworker",
		Short: "Quay repository garbage collection worker",
		Long: `Quay repository garbage collection worker for cleaning up 
deleted repositories and their associated resources.`,
		RunE: runWorker,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Configuration file path")

	// Version command
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("repositorygcworker version %s\n", version)
			fmt.Printf("commit: %s\n", commit)
			fmt.Printf("built: %s\n", buildDate)
		},
	}
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runWorker(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Setup logger
	logger, err := setupLogger(cfg.Logging)
	if err != nil {
		return fmt.Errorf("failed to setup logger: %w", err)
	}
	defer logger.Sync()

	logger.Info("Starting repository GC worker",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("build_date", buildDate))

	// Check feature flags
	if !cfg.Features.RepositoryGarbageCollection {
		logger.Warn("Repository garbage collection is disabled by feature flags")
		return fmt.Errorf("repository garbage collection is disabled")
	}

	// Setup database and Redis connections
	conn, err := database.NewConnection(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to setup database connection: %w", err)
	}
	defer conn.Close()

	// Setup metrics
	metrics := metrics.NewMetrics(&cfg.Metrics, logger)
	if err := metrics.StartServer(&cfg.Metrics); err != nil {
		return fmt.Errorf("failed to start metrics server: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		metrics.Shutdown(ctx)
	}()

	// Setup queue
	q := queue.NewQueue(conn.DB, logger)

	// Create and start worker
	worker := repositorygc.NewWorker(cfg, conn, q, metrics, logger)

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start worker in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Start(ctx)
	}()

	logger.Info("Repository GC worker started successfully")

	// Wait for shutdown signal or error
	select {
	case sig := <-sigCh:
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
		cancel()

		// Give worker time to shut down gracefully
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Workers.ShutdownTimeout)
		defer shutdownCancel()

		if err := worker.Stop(shutdownCtx); err != nil {
			logger.Error("Error during worker shutdown", zap.Error(err))
		} else {
			logger.Info("Worker shut down gracefully")
		}

	case err := <-errCh:
		if err != nil {
			logger.Error("Worker error", zap.Error(err))
			return err
		}
	}

	return nil
}

func setupLogger(cfg config.LoggingConfig) (*zap.Logger, error) {
	// Parse log level
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level %s: %w", cfg.Level, err)
	}

	// Setup encoder config
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.StacktraceKey = "stacktrace"

	// Choose encoder based on format
	var encoder zapcore.Encoder
	switch cfg.Format {
	case "json":
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	case "console":
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	default:
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	// Create core
	core := zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)

	// Create logger with caller info
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return logger, nil
}