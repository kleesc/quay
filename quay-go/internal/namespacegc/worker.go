package namespacegc

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

// JobDetails represents the structure of a namespace GC job
type JobDetails struct {
	MarkerID string `json:"marker_id"`
}

// Worker handles namespace garbage collection
type Worker struct {
	config  *config.Config
	conn    *database.Connection
	queue   *queue.Queue
	metrics *metrics.Metrics
	logger  *zap.Logger
	
	ctx    context.Context
	cancel context.CancelFunc
}

// NewWorker creates a new namespace GC worker
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
		logger:  logger.With(zap.String("worker", "namespace-gc")),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins the worker's main processing loop
func (w *Worker) Start(ctx context.Context) error {
	w.logger.Info("Starting namespace GC worker")
	
	// Update worker count
	w.metrics.WorkersActive.Inc()
	defer w.metrics.WorkersActive.Dec()
	
	// Start metrics reporting
	go w.reportMetrics()
	
	// Start watchdog
	go w.watchdog()
	
	// Main processing loop
	ticker := time.NewTicker(w.config.Queue.NamespaceGC.PollPeriod)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Namespace GC worker shutdown requested")
			return nil
		case <-ticker.C:
			if err := w.pollAndProcess(); err != nil {
				w.logger.Error("Error in namespace GC poll cycle", zap.Error(err))
				w.metrics.RecordWorkerError("namespace-gc", w.config.Queue.NamespaceGC.Name, "poll_error")
			}
		}
	}
}

// Stop gracefully shuts down the worker
func (w *Worker) Stop(ctx context.Context) error {
	w.logger.Info("Stopping namespace GC worker")
	w.cancel()
	return nil
}

// pollAndProcess polls for queue items and processes them
func (w *Worker) pollAndProcess() error {
	// Get next queue item
	item, err := w.queue.GetNextItem(w.ctx, w.config.Queue.NamespaceGC.Name, w.config.Queue.NamespaceGC.Timeout)
	if err != nil {
		return fmt.Errorf("failed to get next queue item: %w", err)
	}
	
	if item == nil {
		// No items available
		return nil
	}
	
	w.logger.Info("Processing namespace GC queue item",
		zap.String("state_id", item.StateID),
		zap.Int("retries_remaining", item.RetriesRemaining))
	
	// Record start time for metrics
	startTime := time.Now()
	
	// Process the item
	err = w.processQueueItem(w.ctx, item)
	
	// Record processing duration
	duration := time.Since(startTime)
	w.metrics.RecordWorkerDuration("namespace-gc", w.config.Queue.NamespaceGC.Name, duration)
	
	if err != nil {
		w.logger.Error("Error processing namespace GC queue item",
			zap.String("state_id", item.StateID),
			zap.Error(err),
			zap.Duration("duration", duration))
		
		// Handle failed item
		retryErr := w.queue.RetryItem(w.ctx, item, w.config.Queue.NamespaceGC.MaxRetries, err)
		if retryErr != nil {
			w.logger.Error("Failed to retry namespace GC queue item",
				zap.String("state_id", item.StateID),
				zap.Error(retryErr))
			w.metrics.RecordWorkerError("namespace-gc", w.config.Queue.NamespaceGC.Name, "retry_error")
			return retryErr
		}
		
		w.metrics.RecordWorkerError("namespace-gc", w.config.Queue.NamespaceGC.Name, "processing_error")
		return nil
	}
	
	// Mark item as completed
	if err := w.queue.CompleteItem(w.ctx, item); err != nil {
		w.logger.Error("Failed to complete namespace GC queue item",
			zap.String("state_id", item.StateID),
			zap.Error(err))
		w.metrics.RecordWorkerError("namespace-gc", w.config.Queue.NamespaceGC.Name, "completion_error")
		return err
	}
	
	w.logger.Info("Successfully processed namespace GC queue item",
		zap.String("state_id", item.StateID),
		zap.Duration("duration", duration))
	
	w.metrics.RecordWorkerProcessed("namespace-gc", w.config.Queue.NamespaceGC.Name)
	return nil
}

// processQueueItem processes a single namespace GC job
func (w *Worker) processQueueItem(ctx context.Context, queueItem *queue.QueueItem) error {
	// Parse job details
	var jobDetails JobDetails
	if err := json.Unmarshal([]byte(queueItem.Body), &jobDetails); err != nil {
		return fmt.Errorf("failed to parse job details: %w", err)
	}
	
	w.logger.Info("Processing namespace GC job", zap.String("marker_id", jobDetails.MarkerID))
	
	// Acquire global lock to prevent concurrent GC operations
	lockKey := "LARGE_GARBAGE_COLLECTION"
	lockTTL := w.config.Queue.NamespaceGC.Timeout + w.config.Queue.NamespaceGC.LockTimeoutPadding
	
	acquired, err := w.acquireGlobalLock(lockKey, lockTTL)
	if err != nil {
		return fmt.Errorf("failed to acquire global lock: %w", err)
	}
	if !acquired {
		return fmt.Errorf("global lock not available")
	}
	
	defer func() {
		if err := w.releaseGlobalLock(lockKey); err != nil {
			w.logger.Error("Error releasing global lock", zap.Error(err))
		}
	}()
	
	// Perform the actual garbage collection
	deleted, err := w.deleteNamespaceViaMarker(ctx, jobDetails.MarkerID)
	if err != nil {
		return fmt.Errorf("failed to delete namespace: %w", err)
	}
	
	if !deleted {
		return fmt.Errorf("namespace GC interrupted; will retry")
	}
	
	w.metrics.RecordNamespaceDeleted()
	w.logger.Info("Successfully deleted namespace", zap.String("marker_id", jobDetails.MarkerID))
	
	return nil
}

// deleteNamespaceViaMarker performs the actual namespace deletion
func (w *Worker) deleteNamespaceViaMarker(ctx context.Context, markerID string) (bool, error) {
	// Start a transaction for the deletion process
	tx, err := w.conn.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	
	// Get the deleted namespace marker
	var marker database.DeletedNamespace
	query := `
		SELECT id, namespace_id, marked, original_username, original_email, queue_id
		FROM deletednamespace 
		WHERE id = $1
	`
	
	err = tx.QueryRowContext(ctx, query, markerID).Scan(
		&marker.ID, &marker.NamespaceID, &marker.Marked,
		&marker.OriginalUsername, &marker.OriginalEmail, &marker.QueueID,
	)
	
	if err == sql.ErrNoRows {
		w.logger.Info("Namespace marker not found, may have been already processed", 
			zap.String("marker_id", markerID))
		return true, nil // Consider it successful if already processed
	}
	if err != nil {
		return false, fmt.Errorf("failed to get namespace marker: %w", err)
	}
	
	w.logger.Info("Deleting namespace", 
		zap.Int("namespace_id", marker.NamespaceID),
		zap.String("original_username", marker.OriginalUsername))
	
	// Delete all repositories under this namespace
	if err := w.deleteNamespaceRepositories(ctx, tx, marker.NamespaceID); err != nil {
		return false, fmt.Errorf("failed to delete namespace repositories: %w", err)
	}
	
	// Delete namespace-related data
	if err := w.deleteNamespaceData(ctx, tx, marker.NamespaceID); err != nil {
		return false, fmt.Errorf("failed to delete namespace data: %w", err)
	}
	
	// Delete the user/namespace itself
	if err := w.deleteUser(ctx, tx, marker.NamespaceID); err != nil {
		return false, fmt.Errorf("failed to delete user: %w", err)
	}
	
	// Delete the marker record
	if err := w.deleteMarker(ctx, tx, marker.ID); err != nil {
		return false, fmt.Errorf("failed to delete marker: %w", err)
	}
	
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit deletion transaction: %w", err)
	}
	
	return true, nil
}

// deleteNamespaceRepositories deletes all repositories under a namespace  
func (w *Worker) deleteNamespaceRepositories(ctx context.Context, tx *sql.Tx, namespaceID int) error {
	// Get all repositories for this namespace
	query := `SELECT id FROM repository WHERE namespace_user_id = $1`
	rows, err := tx.QueryContext(ctx, query, namespaceID)
	if err != nil {
		return fmt.Errorf("failed to query repositories: %w", err)
	}
	defer rows.Close()
	
	var repositoryIDs []int
	for rows.Next() {
		var repoID int
		if err := rows.Scan(&repoID); err != nil {
			return fmt.Errorf("failed to scan repository ID: %w", err)
		}
		repositoryIDs = append(repositoryIDs, repoID)
	}
	
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating repositories: %w", err)
	}
	
	// Delete each repository and its associated data
	for _, repoID := range repositoryIDs {
		w.logger.Info("Deleting repository", zap.Int("repository_id", repoID))
		if err := w.deleteRepository(ctx, tx, repoID); err != nil {
			return fmt.Errorf("failed to delete repository %d: %w", repoID, err)
		}
	}
	
	return nil
}

// deleteRepository deletes a repository and all its associated data
func (w *Worker) deleteRepository(ctx context.Context, tx *sql.Tx, repositoryID int) error {
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
	
	// Delete repository metadata
	tables := []string{
		"repositorynotification",
		"repositorypermission", 
		"repositorybuild",
		"repositorybuildtrigger",
		"quotarepositorysize",
		"star",
		"repositoryautoprunepolicy",
	}
	
	for _, table := range tables {
		query := fmt.Sprintf("DELETE FROM %s WHERE repository_id = $1", table)
		if _, err := tx.ExecContext(ctx, query, repositoryID); err != nil {
			return fmt.Errorf("failed to delete from %s: %w", table, err)
		}
	}
	
	// Finally delete the repository itself
	if _, err := tx.ExecContext(ctx, "DELETE FROM repository WHERE id = $1", repositoryID); err != nil {
		return fmt.Errorf("failed to delete repository: %w", err)
	}
	
	return nil
}

// deleteNamespaceData deletes namespace-related data
func (w *Worker) deleteNamespaceData(ctx context.Context, tx *sql.Tx, namespaceID int) error {
	// Delete team members
	if _, err := tx.ExecContext(ctx, "DELETE FROM teammember WHERE user_id = $1", namespaceID); err != nil {
		return fmt.Errorf("failed to delete team members: %w", err)
	}
	
	// Delete teams where this user is the organization
	if _, err := tx.ExecContext(ctx, "DELETE FROM team WHERE organization_id = $1", namespaceID); err != nil {
		return fmt.Errorf("failed to delete teams: %w", err)
	}
	
	// Delete federated logins
	if _, err := tx.ExecContext(ctx, "DELETE FROM federatedlogin WHERE user_id = $1", namespaceID); err != nil {
		return fmt.Errorf("failed to delete federated logins: %w", err)
	}
	
	// Delete robot account tokens
	if _, err := tx.ExecContext(ctx, "DELETE FROM robotaccounttoken WHERE robot_account_id = $1", namespaceID); err != nil {
		return fmt.Errorf("failed to delete robot account tokens: %w", err)
	}
	
	// Delete robot account metadata
	if _, err := tx.ExecContext(ctx, "DELETE FROM robotaccountmetadata WHERE robot_account_id = $1", namespaceID); err != nil {
		return fmt.Errorf("failed to delete robot account metadata: %w", err)
	}
	
	// Delete quota namespace size
	if _, err := tx.ExecContext(ctx, "DELETE FROM quotanamespacesize WHERE namespace_user_id = $1", namespaceID); err != nil {
		return fmt.Errorf("failed to delete quota namespace size: %w", err)
	}
	
	// Delete namespace auto-prune policy
	if _, err := tx.ExecContext(ctx, "DELETE FROM namespaceautoprunepolicy WHERE namespace_id = $1", namespaceID); err != nil {
		return fmt.Errorf("failed to delete namespace auto-prune policy: %w", err)
	}
	
	// Delete auto-prune task status
	if _, err := tx.ExecContext(ctx, "DELETE FROM autoprunetaskstatus WHERE namespace_id = $1", namespaceID); err != nil {
		return fmt.Errorf("failed to delete auto-prune task status: %w", err)
	}
	
	// Delete OAuth applications
	if _, err := tx.ExecContext(ctx, "DELETE FROM oauthapplication WHERE organization_id = $1", namespaceID); err != nil {
		return fmt.Errorf("failed to delete OAuth applications: %w", err)
	}
	
	// Delete notifications
	if _, err := tx.ExecContext(ctx, "DELETE FROM notification WHERE target_id = $1", namespaceID); err != nil {
		return fmt.Errorf("failed to delete notifications: %w", err)
	}
	
	return nil
}

// deleteUser deletes the user/namespace record
func (w *Worker) deleteUser(ctx context.Context, tx *sql.Tx, userID int) error {
	result, err := tx.ExecContext(ctx, "DELETE FROM \"user\" WHERE id = $1", userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return fmt.Errorf("user %d not found", userID)
	}
	
	return nil
}

// deleteMarker deletes the namespace deletion marker
func (w *Worker) deleteMarker(ctx context.Context, tx *sql.Tx, markerID int) error {
	result, err := tx.ExecContext(ctx, "DELETE FROM deletednamespace WHERE id = $1", markerID)
	if err != nil {
		return fmt.Errorf("failed to delete marker: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return fmt.Errorf("marker %d not found", markerID)
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
			stats, err := w.queue.GetQueueStats(w.ctx, w.config.Queue.NamespaceGC.Name)
			if err != nil {
				w.logger.Error("Failed to get queue stats", zap.Error(err))
				continue
			}
			
			w.metrics.UpdateQueueStats(w.config.Queue.NamespaceGC.Name, stats.Total, stats.Available, 
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
				w.metrics.RecordWorkerError("namespace-gc", w.config.Queue.NamespaceGC.Name, "health_check_failed")
			}
		}
	}
}

// generateLockValue generates a unique value for distributed locks
func generateLockValue() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix())
}