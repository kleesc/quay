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
	"github.com/quay/quay-go/internal/workers"
)

var (
	configPath   string
	workerType   string
	version      = "dev"
	commit       = "unknown"
	buildDate    = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "quay-worker",
		Short: "Quay background worker service",
		Long: `Quay background worker service for processing various tasks like
namespace garbage collection, repository garbage collection, and more.`,
		RunE: runWorker,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Configuration file path")
	rootCmd.PersistentFlags().StringVarP(&workerType, "worker", "w", "", "Worker type (namespace-gc, garbage-collection)")

	// Version command
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("quay-worker version %s\n", version)
			fmt.Printf("commit: %s\n", commit)
			fmt.Printf("built: %s\n", buildDate)
		},
	}
	rootCmd.AddCommand(versionCmd)

	// List workers command
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available worker types",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Available worker types:")
			fmt.Println("  namespace-gc       - Namespace garbage collection worker")
			fmt.Println("  garbage-collection - Repository garbage collection worker")
		},
	}
	rootCmd.AddCommand(listCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runWorker(cmd *cobra.Command, args []string) error {
	// Validate worker type
	if workerType == "" {
		return fmt.Errorf("worker type is required (use --worker flag)")
	}

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

	logger.Info("Starting Quay worker",
		zap.String("worker_type", workerType),
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("build_date", buildDate))

	// Check feature flags
	if !isWorkerEnabled(workerType, cfg.Features) {
		logger.Warn("Worker type is disabled by feature flags",
			zap.String("worker_type", workerType))
		return fmt.Errorf("worker type %s is disabled", workerType)
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
	worker, err := createWorker(workerType, cfg, conn, q, metrics, logger)
	if err != nil {
		return fmt.Errorf("failed to create worker: %w", err)
	}

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

	logger.Info("Worker started successfully", zap.String("worker_type", workerType))

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

func createWorker(workerType string, cfg *config.Config, conn *database.Connection, q *queue.Queue, m *metrics.Metrics, logger *zap.Logger) (workers.Worker, error) {
	switch workerType {
	case "namespace-gc":
		return workers.NewNamespaceGCWorker(cfg, conn, q, m, logger), nil
	case "garbage-collection":
		return workers.NewGarbageCollectionWorker(cfg, conn, q, m, logger), nil
	default:
		return nil, fmt.Errorf("unknown worker type: %s", workerType)
	}
}

func isWorkerEnabled(workerType string, features config.FeatureConfig) bool {
	switch workerType {
	case "namespace-gc":
		return features.NamespaceGarbageCollection
	case "garbage-collection":
		return features.GarbageCollection
	default:
		return false
	}
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