package repositorygc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/quay/quay-go/internal/config"
	"github.com/quay/quay-go/internal/database"
	"github.com/quay/quay-go/internal/metrics"
	"github.com/quay/quay-go/internal/queue"
)

const (
	// RepositoryGCTimeout is the timeout for repository garbage collection
	RepositoryGCTimeout = 3 * time.Hour
	// LockTimeoutPadding is the padding added to lock timeout
	LockTimeoutPadding = 60 * time.Second
)

// JobDetails represents the structure of a repository GC job
type JobDetails struct {
	MarkerID string `json:"marker_id"`
}

// Worker handles repository garbage collection
type Worker struct {
	config  *config.Config
	conn    *database.Connection
	queue   *queue.Queue
	metrics *metrics.Metrics
	logger  *zap.Logger
	
	ctx    context.Context
	cancel context.CancelFunc
}

// NewWorker creates a new repository GC worker
func NewWorker(
	cfg *config.Config,
	conn *database.Connection,
	q *queue.Queue,
	m *metrics.Metrics,
	logger *zap.Logger,
) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Worker{
		config:  cfg,
		conn:    conn,
		queue:   q,
		metrics: m,
		logger:  logger.With(zap.String("worker", "repository-gc")),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins the worker's main processing loop
func (w *Worker) Start(ctx context.Context) error {
	w.logger.Info("Starting repository GC worker")
	
	// Update worker count
	w.metrics.WorkersActive.Inc()
	defer w.metrics.WorkersActive.Dec()
	
	// Start metrics reporting
	go w.reportMetrics()
	
	// Start watchdog
	go w.watchdog()
	
	// Main processing loop
	ticker := time.NewTicker(w.config.Queue.RepositoryGC.PollPeriod)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Repository GC worker shutdown requested")
			return nil
		case <-ticker.C:
			if err := w.pollAndProcess(); err != nil {
				w.logger.Error("Error in repository GC poll cycle", zap.Error(err))
				w.metrics.RecordWorkerError("repository-gc", w.config.Queue.RepositoryGC.Name, "poll_error")
			}
		}
	}
}

// Stop gracefully shuts down the worker
func (w *Worker) Stop(ctx context.Context) error {
	w.logger.Info("Stopping repository GC worker")
	w.cancel()
	return nil
}

// pollAndProcess polls for queue items and processes them
func (w *Worker) pollAndProcess() error {
	// Get next queue item
	item, err := w.queue.GetNextItem(w.ctx, w.config.Queue.RepositoryGC.Name, w.config.Queue.RepositoryGC.Timeout)
	if err != nil {
		return fmt.Errorf("failed to get next queue item: %w", err)
	}
	
	if item == nil {
		// No items available
		return nil
	}
	
	w.logger.Info("Processing repository GC queue item",
		zap.String("state_id", item.StateID),
		zap.Int("retries_remaining", item.RetriesRemaining))
	
	// Record start time for metrics
	startTime := time.Now()
	
	// Process the item
	err = w.processQueueItem(w.ctx, item)
	
	// Record processing duration
	duration := time.Since(startTime)
	w.metrics.RecordWorkerDuration("repository-gc", w.config.Queue.RepositoryGC.Name, duration)
	
	if err != nil {
		w.logger.Error("Error processing repository GC queue item",
			zap.String("state_id", item.StateID),
			zap.Error(err),
			zap.Duration("duration", duration))
		
		// Handle failed item
		retryErr := w.queue.RetryItem(w.ctx, item, w.config.Queue.RepositoryGC.MaxRetries, err)
		if retryErr != nil {
			w.logger.Error("Failed to retry repository GC queue item",
				zap.String("state_id", item.StateID),
				zap.Error(retryErr))
			w.metrics.RecordWorkerError("repository-gc", w.config.Queue.RepositoryGC.Name, "retry_error")
			return retryErr
		}
		
		w.metrics.RecordWorkerError("repository-gc", w.config.Queue.RepositoryGC.Name, "processing_error")
		return nil
	}
	
	// Mark item as completed
	if err := w.queue.CompleteItem(w.ctx, item); err != nil {
		w.logger.Error("Failed to complete repository GC queue item",
			zap.String("state_id", item.StateID),
			zap.Error(err))
		w.metrics.RecordWorkerError("repository-gc", w.config.Queue.RepositoryGC.Name, "completion_error")
		return err
	}
	
	w.logger.Info("Successfully processed repository GC queue item",
		zap.String("state_id", item.StateID),
		zap.Duration("duration", duration))
	
	w.metrics.RecordWorkerProcessed("repository-gc", w.config.Queue.RepositoryGC.Name)
	return nil
}

// processQueueItem processes a single repository GC job
func (w *Worker) processQueueItem(ctx context.Context, queueItem *queue.QueueItem) error {
	// Parse job details
	var jobDetails JobDetails
	if err := json.Unmarshal([]byte(queueItem.Body), &jobDetails); err != nil {
		return fmt.Errorf("failed to parse job details: %w", err)
	}
	
	w.logger.Debug("Processing repository GC job", zap.String("marker_id", jobDetails.MarkerID))
	
	// Acquire global lock to prevent concurrent GC operations
	lockKey := "LARGE_GARBAGE_COLLECTION"
	lockTTL := RepositoryGCTimeout + LockTimeoutPadding
	
	acquired, err := w.acquireGlobalLock(lockKey, lockTTL)
	if err != nil {
		return fmt.Errorf("failed to acquire global lock: %w", err)
	}
	if !acquired {
		w.logger.Debug("Could not acquire global lock for garbage collection")
		return fmt.Errorf("global lock not available")
	}
	
	defer func() {
		if err := w.releaseGlobalLock(lockKey); err != nil {
			w.logger.Error("Error releasing global lock", zap.Error(err))
		}
	}()
	
	// Perform the actual garbage collection
	if err := w.performGC(ctx, jobDetails); err != nil {
		return fmt.Errorf("failed to perform GC: %w", err)
	}
	
	w.metrics.RecordRepositoryDeleted()
	w.logger.Debug("Successfully processed repository GC job", zap.String("marker_id", jobDetails.MarkerID))
	
	return nil
}

// performGC performs the actual repository garbage collection
func (w *Worker) performGC(ctx context.Context, jobDetails JobDetails) error {
	w.logger.Debug("Got repository GC queue item", zap.String("marker_id", jobDetails.MarkerID))
	
	// Get the deleted repository marker
	var marker database.DeletedRepository
	query := `
		SELECT id, repository_id, marked, original_name, original_namespace, queue_id
		FROM deletedrepository 
		WHERE id = $1
	`
	
	err := w.conn.DB.QueryRowContext(ctx, query, jobDetails.MarkerID).Scan(
		&marker.ID, &marker.RepositoryID, &marker.Marked,
		&marker.OriginalName, &marker.OriginalNamespace, &marker.QueueID,
	)
	
	if err == sql.ErrNoRows {
		w.logger.Debug("Found no matching delete repo marker", zap.String("marker_id", jobDetails.MarkerID))
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get repository marker: %w", err)
	}
	
	// Get the repository details
	var repository database.Repository
	repoQuery := `
		SELECT id, name, namespace_user_id, description, visibility_id, 
			   badge_token, trust_enabled, kind_id, state
		FROM repository 
		WHERE id = $1
	`
	
	err = w.conn.DB.QueryRowContext(ctx, repoQuery, marker.RepositoryID).Scan(
		&repository.ID, &repository.Name, &repository.NamespaceUserID, &repository.Description,
		&repository.VisibilityID, &repository.BadgeToken, &repository.TrustEnabled, 
		&repository.KindID, &repository.State,
	)
	
	if err == sql.ErrNoRows {
		w.logger.Debug("Repository already deleted", zap.Int("repository_id", marker.RepositoryID))
		// Delete the marker since the repository is already gone
		return w.deleteMarker(ctx, marker.ID)
	}
	if err != nil {
		return fmt.Errorf("failed to get repository: %w", err)
	}
	
	w.logger.Debug("Purging repository", 
		zap.Int("repository_id", repository.ID),
		zap.String("repository_name", repository.Name))
	
	// Purge the repository
	if !w.purgeRepository(ctx, &repository) {
		return fmt.Errorf("GC interrupted; will retry")
	}
	
	// Delete the marker
	return w.deleteMarker(ctx, marker.ID)
}

// purgeRepository deletes a repository and all its associated data
func (w *Worker) purgeRepository(ctx context.Context, repository *database.Repository) bool {
	tx, err := w.conn.DB.BeginTx(ctx, nil)
	if err != nil {
		w.logger.Error("Failed to begin transaction", zap.Error(err))
		return false
	}
	defer tx.Rollback()
	
	// Delete repository data in dependency order
	if err := w.deleteRepositoryData(ctx, tx, repository.ID); err != nil {
		w.logger.Error("Failed to delete repository data", zap.Error(err))
		return false
	}
	
	// Delete the repository itself
	if _, err := tx.ExecContext(ctx, "DELETE FROM repository WHERE id = $1", repository.ID); err != nil {
		w.logger.Error("Failed to delete repository", zap.Error(err))
		return false
	}
	
	if err := tx.Commit(); err != nil {
		w.logger.Error("Failed to commit repository deletion", zap.Error(err))
		return false
	}
	
	w.logger.Debug("Successfully purged repository", 
		zap.Int("repository_id", repository.ID),
		zap.String("repository_name", repository.Name))
	
	return true
}

// deleteRepositoryData deletes all data associated with a repository
func (w *Worker) deleteRepositoryData(ctx context.Context, tx *sql.Tx, repositoryID int) error {
	// Delete in dependency order to avoid foreign key violations
	
	// Delete tags
	if _, err := tx.ExecContext(ctx, "DELETE FROM tag WHERE repository_id = $1", repositoryID); err != nil {
		return fmt.Errorf("failed to delete tags: %w", err)
	}
	
	// Delete manifest blobs
	if _, err := tx.ExecContext(ctx, "DELETE FROM manifestblob WHERE repository_id = $1", repositoryID); err != nil {
		return fmt.Errorf("failed to delete manifest blobs: %w", err)
	}
	
	// Delete manifest labels
	if _, err := tx.ExecContext(ctx, "DELETE FROM manifestlabel WHERE repository_id = $1", repositoryID); err != nil {
		return fmt.Errorf("failed to delete manifest labels: %w", err)
	}
	
	// Delete manifest children
	if _, err := tx.ExecContext(ctx, "DELETE FROM manifestchild WHERE repository_id = $1", repositoryID); err != nil {
		return fmt.Errorf("failed to delete manifest children: %w", err)
	}
	
	// Delete manifests
	if _, err := tx.ExecContext(ctx, "DELETE FROM manifest WHERE repository_id = $1", repositoryID); err != nil {
		return fmt.Errorf("failed to delete manifests: %w", err)
	}
	
	// Delete uploaded blobs
	if _, err := tx.ExecContext(ctx, "DELETE FROM uploadedblob WHERE repository_id = $1", repositoryID); err != nil {
		return fmt.Errorf("failed to delete uploaded blobs: %w", err)
	}
	
	// Delete repository metadata
	tables := []string{
		"repositorynotification",
		"repositorypermission", 
		"repositorybuild",
		"repositorybuildtrigger",
		"quotarepositorysize",
		"star",
		"repositoryautoprunepolicy",
		"apprepositorybuild",
		"repositoryauthorizedemail",
		"repositorybuildtrigger",
		"logentry",
	}
	
	for _, table := range tables {
		query := fmt.Sprintf("DELETE FROM %s WHERE repository_id = $1", table)
		if _, err := tx.ExecContext(ctx, query, repositoryID); err != nil {
			// Some tables might not exist or have different column names
			// Log the error but continue
			w.logger.Debug("Failed to delete from table", 
				zap.String("table", table), 
				zap.Error(err))
		}
	}
	
	return nil
}

// deleteMarker deletes the repository deletion marker
func (w *Worker) deleteMarker(ctx context.Context, markerID int) error {
	result, err := w.conn.DB.ExecContext(ctx, "DELETE FROM deletedrepository WHERE id = $1", markerID)
	if err != nil {
		return fmt.Errorf("failed to delete marker: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		w.logger.Debug("Marker already deleted", zap.Int("marker_id", markerID))
	}
	
	return nil
}

// acquireGlobalLock acquires a distributed lock using Redis
func (w *Worker) acquireGlobalLock(key string, ttl time.Duration) (bool, error) {
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

// releaseGlobalLock releases a distributed lock
func (w *Worker) releaseGlobalLock(key string) error {
	if w.conn.RedisClient == nil {
		return nil
	}
	
	result := w.conn.RedisClient.Del(w.ctx, key)
	return result.Err()
}

// reportMetrics periodically reports worker metrics
func (w *Worker) reportMetrics() {
	ticker := time.NewTicker(w.config.Workers.MetricsInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			// Update queue statistics
			stats, err := w.queue.GetQueueStats(w.ctx, w.config.Queue.RepositoryGC.Name)
			if err != nil {
				w.logger.Error("Failed to get queue stats", zap.Error(err))
				continue
			}
			
			w.metrics.UpdateQueueStats(w.config.Queue.RepositoryGC.Name, stats.Total, stats.Available, 
				stats.Processing, stats.Expired, stats.Failed)
			
			// Update database connection metrics
			dbStats := w.conn.DB.Stats()
			w.metrics.UpdateDatabaseConnections(dbStats.OpenConnections, 
				dbStats.Idle, dbStats.InUse)
		}
	}
}

// watchdog monitors worker health
func (w *Worker) watchdog() {
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
				w.metrics.RecordWorkerError("repository-gc", w.config.Queue.RepositoryGC.Name, "health_check_failed")
			}
		}
	}
}

// generateLockValue generates a unique value for distributed locks
func generateLockValue() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix())
}