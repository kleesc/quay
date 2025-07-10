package workers

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/quay/quay-go/internal/config"
	"github.com/quay/quay-go/internal/database"
	"github.com/quay/quay-go/internal/metrics"
	"github.com/quay/quay-go/internal/queue"
)

// Worker represents a generic worker interface
type Worker interface {
	// Start begins the worker's processing loop
	Start(ctx context.Context) error
	
	// Stop gracefully shuts down the worker
	Stop(ctx context.Context) error
	
	// Name returns the worker's name
	Name() string
	
	// QueueName returns the queue name this worker processes
	QueueName() string
}

// WorkerBase provides common functionality for all workers
type WorkerBase struct {
	name        string
	queueName   string
	config      *config.Config
	conn        *database.Connection
	queue       *queue.Queue
	metrics     *metrics.Metrics
	logger      *zap.Logger
	
	ctx    context.Context
	cancel context.CancelFunc
}

// NewWorkerBase creates a new worker base
func NewWorkerBase(
	name, queueName string,
	cfg *config.Config,
	conn *database.Connection,
	q *queue.Queue,
	m *metrics.Metrics,
	logger *zap.Logger,
) *WorkerBase {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &WorkerBase{
		name:      name,
		queueName: queueName,
		config:    cfg,
		conn:      conn,
		queue:     q,
		metrics:   m,
		logger:    logger.With(zap.String("worker", name)),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Name returns the worker name
func (w *WorkerBase) Name() string {
	return w.name
}

// QueueName returns the queue name
func (w *WorkerBase) QueueName() string {
	return w.queueName
}

// Stop gracefully shuts down the worker
func (w *WorkerBase) Stop(ctx context.Context) error {
	w.logger.Info("Stopping worker")
	w.cancel()
	return nil
}

// AcquireGlobalLock acquires a distributed lock using Redis
func (w *WorkerBase) AcquireGlobalLock(key string, ttl time.Duration) (bool, error) {
	if w.conn.RedisClient == nil {
		w.logger.Warn("Redis client not available, proceeding without lock")
		return true, nil
	}
	
	lockValue := generateLockValue()
	result := w.conn.RedisClient.SetNX(w.ctx, key, lockValue, ttl)
	if result.Err() != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", result.Err())
	}
	
	return result.Val(), nil
}

// ReleaseGlobalLock releases a distributed lock
func (w *WorkerBase) ReleaseGlobalLock(key string) error {
	if w.conn.RedisClient == nil {
		return nil
	}
	
	result := w.conn.RedisClient.Del(w.ctx, key)
	return result.Err()
}

// ProcessItem represents the interface for processing queue items
type ProcessItem func(ctx context.Context, item *queue.QueueItem) error

// Run starts the worker's main processing loop
func (w *WorkerBase) Run(processItem ProcessItem, pollPeriod time.Duration, timeout time.Duration, maxRetries int) error {
	w.logger.Info("Starting worker",
		zap.String("queue", w.queueName),
		zap.Duration("poll_period", pollPeriod),
		zap.Duration("timeout", timeout),
		zap.Int("max_retries", maxRetries))
	
	// Start metrics reporting
	go w.reportMetrics()
	
	// Start watchdog
	go w.watchdog()
	
	// Update worker count
	w.metrics.WorkersActive.Inc()
	defer w.metrics.WorkersActive.Dec()
	
	// Main processing loop
	ticker := time.NewTicker(pollPeriod)
	defer ticker.Stop()
	
	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info("Worker shutdown requested")
			return nil
		case <-ticker.C:
			if err := w.pollAndProcess(processItem, timeout, maxRetries); err != nil {
				w.logger.Error("Error in worker poll cycle", zap.Error(err))
				w.metrics.RecordWorkerError(w.name, w.queueName, "poll_error")
			}
		}
	}
}

// pollAndProcess polls for queue items and processes them
func (w *WorkerBase) pollAndProcess(processItem ProcessItem, timeout time.Duration, maxRetries int) error {
	// Get next queue item
	item, err := w.queue.GetNextItem(w.ctx, w.queueName, timeout)
	if err != nil {
		return fmt.Errorf("failed to get next queue item: %w", err)
	}
	
	if item == nil {
		// No items available
		return nil
	}
	
	w.logger.Info("Processing queue item",
		zap.String("state_id", item.StateID),
		zap.Int("retries_remaining", item.RetriesRemaining))
	
	// Record start time for metrics
	startTime := time.Now()
	
	// Process the item
	err = processItem(w.ctx, item)
	
	// Record processing duration
	duration := time.Since(startTime)
	w.metrics.RecordWorkerDuration(w.name, w.queueName, duration)
	
	if err != nil {
		w.logger.Error("Error processing queue item",
			zap.String("state_id", item.StateID),
			zap.Error(err),
			zap.Duration("duration", duration))
		
		// Handle failed item
		retryErr := w.queue.RetryItem(w.ctx, item, maxRetries, err)
		if retryErr != nil {
			w.logger.Error("Failed to retry queue item",
				zap.String("state_id", item.StateID),
				zap.Error(retryErr))
			w.metrics.RecordWorkerError(w.name, w.queueName, "retry_error")
			return retryErr
		}
		
		w.metrics.RecordWorkerError(w.name, w.queueName, "processing_error")
		return nil
	}
	
	// Mark item as completed
	if err := w.queue.CompleteItem(w.ctx, item); err != nil {
		w.logger.Error("Failed to complete queue item",
			zap.String("state_id", item.StateID),
			zap.Error(err))
		w.metrics.RecordWorkerError(w.name, w.queueName, "completion_error")
		return err
	}
	
	w.logger.Info("Successfully processed queue item",
		zap.String("state_id", item.StateID),
		zap.Duration("duration", duration))
	
	w.metrics.RecordWorkerProcessed(w.name, w.queueName)
	return nil
}

// reportMetrics periodically reports worker metrics
func (w *WorkerBase) reportMetrics() {
	ticker := time.NewTicker(w.config.Workers.MetricsInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			// Update queue statistics
			stats, err := w.queue.GetQueueStats(w.ctx, w.queueName)
			if err != nil {
				w.logger.Error("Failed to get queue stats", zap.Error(err))
				continue
			}
			
			w.metrics.UpdateQueueStats(w.queueName, stats.Total, stats.Available, 
				stats.Processing, stats.Expired, stats.Failed)
			
			// Update database connection metrics
			dbStats := w.conn.DB.Stats()
			w.metrics.UpdateDatabaseConnections(dbStats.OpenConnections, 
				dbStats.Idle, dbStats.InUse)
		}
	}
}

// watchdog monitors worker health
func (w *WorkerBase) watchdog() {
	ticker := time.NewTicker(w.config.Workers.WatchdogPeriod)
	defer ticker.Stop()
	
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			// Perform health checks
			if err := w.conn.HealthCheck(w.ctx); err != nil {
				w.logger.Error("Worker health check failed", zap.Error(err))
				w.metrics.RecordWorkerError(w.name, w.queueName, "health_check_failed")
			}
		}
	}
}

// generateLockValue generates a unique value for distributed locks
func generateLockValue() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix())
}