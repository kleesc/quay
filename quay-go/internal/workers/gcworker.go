package workers

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"

	"github.com/quay/quay-go/internal/config"
	"github.com/quay/quay-go/internal/database"
	"github.com/quay/quay-go/internal/metrics"
	"github.com/quay/quay-go/internal/queue"
)

const (
	// DefaultGCFrequency is the default garbage collection frequency in seconds
	DefaultGCFrequency = 30
	// GCCandidateCount is the number of repository candidates to consider for GC
	GCCandidateCount = 500
	// GCChunkSize is the size of chunks when processing items for deletion
	GCChunkSize = 100
)

// GarbageCollectionWorker handles repository garbage collection
type GarbageCollectionWorker struct {
	*WorkerBase
	gcFrequency time.Duration
}

// NewGarbageCollectionWorker creates a new garbage collection worker
func NewGarbageCollectionWorker(
	cfg *config.Config,
	conn *database.Connection,
	q *queue.Queue,
	m *metrics.Metrics,
	logger *zap.Logger,
) *GarbageCollectionWorker {
	base := NewWorkerBase("garbage-collection", "", cfg, conn, q, m, logger)
	
	frequency := time.Duration(cfg.Workers.GCFrequency) * time.Second
	if frequency == 0 {
		frequency = DefaultGCFrequency * time.Second
	}
	
	return &GarbageCollectionWorker{
		WorkerBase:  base,
		gcFrequency: frequency,
	}
}

// Start begins the worker's main processing loop
func (w *GarbageCollectionWorker) Start(ctx context.Context) error {
	w.logger.Info("Starting garbage collection worker",
		zap.Duration("frequency", w.gcFrequency))
	
	// Update worker count
	w.metrics.WorkersActive.Inc()
	defer w.metrics.WorkersActive.Dec()
	
	// Start metrics reporting
	go w.reportMetrics()
	
	// Start watchdog
	go w.watchdog()
	
	// Main processing loop
	ticker := time.NewTicker(w.gcFrequency)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Garbage collection worker shutdown requested")
			return nil
		case <-ticker.C:
			if err := w.performGarbageCollection(ctx); err != nil {
				w.logger.Error("Error performing garbage collection", zap.Error(err))
				w.metrics.RecordWorkerError(w.name, "gc", "collection_error")
			}
			
			// Perform notification scanning if enabled
			if w.config.Features.ImageExpiryTrigger {
				if err := w.scanImageExpiryNotifications(ctx); err != nil {
					w.logger.Error("Error scanning image expiry notifications", zap.Error(err))
					w.metrics.RecordWorkerError(w.name, "gc", "notification_error")
				}
			}
		}
	}
}

// performGarbageCollection performs garbage collection on repositories
func (w *GarbageCollectionWorker) performGarbageCollection(ctx context.Context) error {
	// Get a random GC policy
	policy, err := w.getRandomGCPolicy(ctx)
	if err != nil {
		return fmt.Errorf("failed to get GC policy: %w", err)
	}
	
	if policy == nil {
		w.logger.Debug("No GC policies found")
		return nil
	}
	
	// Find a repository with garbage
	repo, err := w.findRepositoryWithGarbage(ctx, *policy)
	if err != nil {
		return fmt.Errorf("failed to find repository with garbage: %w", err)
	}
	
	if repo == nil {
		w.logger.Debug("No repository with garbage found")
		return nil
	}
	
	w.logger.Debug("Found repository for garbage collection",
		zap.Int("repository_id", repo.ID),
		zap.String("repository_name", repo.Name))
	
	// Acquire global lock for this repository
	lockKey := fmt.Sprintf("REPO_GARBAGE_COLLECTION_%d", repo.ID)
	lockTTL := 3*time.Hour + 60*time.Second
	
	acquired, err := w.AcquireGlobalLock(lockKey, lockTTL)
	if err != nil {
		return fmt.Errorf("failed to acquire global lock: %w", err)
	}
	if !acquired {
		w.logger.Debug("Could not acquire repo lock for garbage collection",
			zap.Int("repository_id", repo.ID))
		return nil
	}
	
	defer func() {
		if err := w.ReleaseGlobalLock(lockKey); err != nil {
			w.logger.Error("Error releasing global lock", zap.Error(err))
		}
	}()
	
	// Perform garbage collection
	w.logger.Debug("Starting GC of repository",
		zap.Int("repository_id", repo.ID),
		zap.String("repository_name", repo.Name))
	
	hadChanges, err := w.garbageCollectRepository(ctx, repo)
	if err != nil {
		return fmt.Errorf("failed to garbage collect repository: %w", err)
	}
	
	if hadChanges {
		w.metrics.RecordRepositoryGC(repo.Name)
		w.logger.Debug("Finished GC of repository",
			zap.Int("repository_id", repo.ID),
			zap.String("repository_name", repo.Name))
	}
	
	return nil
}

// getRandomGCPolicy returns a random GC policy from the database
func (w *GarbageCollectionWorker) getRandomGCPolicy(ctx context.Context) (*int, error) {
	query := `
		SELECT DISTINCT removed_tag_expiration_s 
		FROM namespace 
		WHERE removed_tag_expiration_s IS NOT NULL 
		LIMIT 100
	`
	
	rows, err := w.conn.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query GC policies: %w", err)
	}
	defer rows.Close()
	
	var policies []int
	for rows.Next() {
		var policy int
		if err := rows.Scan(&policy); err != nil {
			return nil, fmt.Errorf("failed to scan GC policy: %w", err)
		}
		policies = append(policies, policy)
	}
	
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating GC policies: %w", err)
	}
	
	if len(policies) == 0 {
		return nil, nil
	}
	
	// Return random policy
	policy := policies[rand.Intn(len(policies))]
	return &policy, nil
}

// findRepositoryWithGarbage finds a repository that has garbage to collect
func (w *GarbageCollectionWorker) findRepositoryWithGarbage(ctx context.Context, gcPolicyS int) (*database.Repository, error) {
	currentTime := time.Now().UnixNano() / int64(time.Millisecond)
	expirationTimestamp := currentTime - (int64(gcPolicyS) * 1000)
	
	query := `
		SELECT DISTINCT r.id, r.name, r.namespace_user_id, r.description, r.visibility_id, 
			   r.badge_token, r.trust_enabled, r.kind_id, r.state
		FROM tag t
		JOIN repository r ON t.repository_id = r.id
		JOIN namespace n ON r.namespace_user_id = n.id
		WHERE t.lifetime_end_ms IS NOT NULL
		  AND t.lifetime_end_ms <= $1
		  AND n.removed_tag_expiration_s = $2
		  AND n.enabled = true
		  AND r.state != $3
		ORDER BY RANDOM()
		LIMIT 1
	`
	
	var repo database.Repository
	err := w.conn.DB.QueryRowContext(ctx, query, expirationTimestamp, gcPolicyS, database.RepositoryStateMarkedForDeletion).Scan(
		&repo.ID, &repo.Name, &repo.NamespaceUserID, &repo.Description,
		&repo.VisibilityID, &repo.BadgeToken, &repo.TrustEnabled, &repo.KindID, &repo.State,
	)
	
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find repository with garbage: %w", err)
	}
	
	return &repo, nil
}

// garbageCollectRepository performs garbage collection on a specific repository
func (w *GarbageCollectionWorker) garbageCollectRepository(ctx context.Context, repo *database.Repository) (bool, error) {
	hadChanges := false
	
	// Purge expired tags
	expiredTags, err := w.lookupUnrecoverableTags(ctx, repo.ID)
	if err != nil {
		return false, fmt.Errorf("failed to lookup unrecoverable tags: %w", err)
	}
	
	if len(expiredTags) > 0 {
		w.logger.Debug("Found expired tags to GC",
			zap.Int("count", len(expiredTags)),
			zap.Int("repository_id", repo.ID))
		
		if err := w.purgeExpiredTags(ctx, expiredTags); err != nil {
			return false, fmt.Errorf("failed to purge expired tags: %w", err)
		}
		hadChanges = true
	}
	
	// Purge expired uploaded blobs
	expiredBlobs, err := w.lookupExpiredUploadedBlobs(ctx, repo.ID)
	if err != nil {
		return false, fmt.Errorf("failed to lookup expired uploaded blobs: %w", err)
	}
	
	if len(expiredBlobs) > 0 {
		w.logger.Debug("Found expired uploaded blobs to GC",
			zap.Int("count", len(expiredBlobs)),
			zap.Int("repository_id", repo.ID))
		
		if err := w.purgeExpiredUploadedBlobs(ctx, expiredBlobs); err != nil {
			return false, fmt.Errorf("failed to purge expired uploaded blobs: %w", err)
		}
		hadChanges = true
	}
	
	return hadChanges, nil
}

// lookupUnrecoverableTags finds tags that are expired and past their recovery period
func (w *GarbageCollectionWorker) lookupUnrecoverableTags(ctx context.Context, repositoryID int) ([]database.Tag, error) {
	query := `
		SELECT t.id, t.name, t.repository_id, t.lifetime_start_ms, t.lifetime_end_ms,
			   t.hidden, t.manifest_id, t.reversion, t.tag_kind_id
		FROM tag t
		JOIN repository r ON t.repository_id = r.id
		JOIN namespace n ON r.namespace_user_id = n.id
		WHERE t.repository_id = $1
		  AND t.lifetime_end_ms IS NOT NULL
		  AND t.lifetime_end_ms <= (EXTRACT(EPOCH FROM NOW()) * 1000 - n.removed_tag_expiration_s * 1000)
		LIMIT $2
	`
	
	rows, err := w.conn.DB.QueryContext(ctx, query, repositoryID, GCChunkSize)
	if err != nil {
		return nil, fmt.Errorf("failed to query unrecoverable tags: %w", err)
	}
	defer rows.Close()
	
	var tags []database.Tag
	for rows.Next() {
		var tag database.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.RepositoryID, &tag.LifetimeStartMS,
			&tag.LifetimeEndMS, &tag.Hidden, &tag.ManifestID, &tag.Reversion, &tag.TagKindID); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tags: %w", err)
	}
	
	return tags, nil
}

// lookupExpiredUploadedBlobs finds expired uploaded blobs in a repository
func (w *GarbageCollectionWorker) lookupExpiredUploadedBlobs(ctx context.Context, repositoryID int) ([]database.UploadedBlob, error) {
	query := `
		SELECT id, repository_id, blob_id, byte_count, uncompressed_byte_count,
			   upload_state, sha_state, piece_hashes, storage_metadata, uuid, 
			   location_id, expires_at, created
		FROM uploadedblob
		WHERE repository_id = $1
		  AND expires_at <= NOW()
		LIMIT $2
	`
	
	rows, err := w.conn.DB.QueryContext(ctx, query, repositoryID, GCChunkSize)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired uploaded blobs: %w", err)
	}
	defer rows.Close()
	
	var blobs []database.UploadedBlob
	for rows.Next() {
		var blob database.UploadedBlob
		if err := rows.Scan(&blob.ID, &blob.RepositoryID, &blob.BlobID, &blob.ByteCount,
			&blob.UncompressedByteCount, &blob.UploadState, &blob.SHAState, &blob.PieceHashes,
			&blob.StorageMetadata, &blob.UUID, &blob.LocationID, &blob.ExpiresAt, &blob.Created); err != nil {
			return nil, fmt.Errorf("failed to scan uploaded blob: %w", err)
		}
		blobs = append(blobs, blob)
	}
	
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating uploaded blobs: %w", err)
	}
	
	return blobs, nil
}

// purgeExpiredTags removes expired tags from the database
func (w *GarbageCollectionWorker) purgeExpiredTags(ctx context.Context, tags []database.Tag) error {
	if len(tags) == 0 {
		return nil
	}
	
	tx, err := w.conn.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	
	// Delete tags in chunks
	for _, tag := range tags {
		w.logger.Debug("Deleting expired tag",
			zap.Int("tag_id", tag.ID),
			zap.String("tag_name", tag.Name),
			zap.Int("repository_id", tag.RepositoryID))
		
		if err := w.deleteTag(ctx, tx, tag.ID); err != nil {
			return fmt.Errorf("failed to delete tag %d: %w", tag.ID, err)
		}
	}
	
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit tag deletion transaction: %w", err)
	}
	
	return nil
}

// purgeExpiredUploadedBlobs removes expired uploaded blobs from the database
func (w *GarbageCollectionWorker) purgeExpiredUploadedBlobs(ctx context.Context, blobs []database.UploadedBlob) error {
	if len(blobs) == 0 {
		return nil
	}
	
	tx, err := w.conn.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	
	// Delete uploaded blobs
	for _, blob := range blobs {
		w.logger.Debug("Deleting expired uploaded blob",
			zap.Int64("blob_id", blob.ID),
			zap.String("uuid", blob.UUID),
			zap.Int("repository_id", blob.RepositoryID))
		
		if _, err := tx.ExecContext(ctx, "DELETE FROM uploadedblob WHERE id = $1", blob.ID); err != nil {
			return fmt.Errorf("failed to delete uploaded blob %d: %w", blob.ID, err)
		}
	}
	
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit uploaded blob deletion transaction: %w", err)
	}
	
	return nil
}

// deleteTag removes a tag and its associated data
func (w *GarbageCollectionWorker) deleteTag(ctx context.Context, tx *sql.Tx, tagID int) error {
	// Delete the tag
	if _, err := tx.ExecContext(ctx, "DELETE FROM tag WHERE id = $1", tagID); err != nil {
		return fmt.Errorf("failed to delete tag: %w", err)
	}
	
	return nil
}

// scanImageExpiryNotifications scans for image expiry notifications
func (w *GarbageCollectionWorker) scanImageExpiryNotifications(ctx context.Context) error {
	// This is a simplified version - in a full implementation, you would:
	// 1. Query for repository notifications that need processing
	// 2. Find tags that are expiring based on notification config
	// 3. Send notifications to users
	// 4. Track which tags have been notified to avoid duplicates
	
	w.logger.Debug("Scanning for image expiry notifications")
	
	// For now, just log that we would scan for notifications
	// In a real implementation, this would be more complex
	query := `
		SELECT COUNT(*) 
		FROM repositorynotification rn
		JOIN externalnotificationevent ene ON rn.event_id = ene.id
		WHERE ene.name = 'repo_image_expiry'
		  AND rn.method_id IS NOT NULL
	`
	
	var count int
	if err := w.conn.DB.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return fmt.Errorf("failed to count image expiry notifications: %w", err)
	}
	
	if count > 0 {
		w.logger.Debug("Found image expiry notifications to process", zap.Int("count", count))
		// In a real implementation, process these notifications
	}
	
	return nil
}

// reportMetrics periodically reports GC worker metrics
func (w *GarbageCollectionWorker) reportMetrics() {
	ticker := time.NewTicker(w.config.Workers.MetricsInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			// Update database connection metrics
			dbStats := w.conn.DB.Stats()
			w.metrics.UpdateDatabaseConnections(dbStats.OpenConnections,
				dbStats.Idle, dbStats.InUse)
		}
	}
}

// watchdog monitors worker health
func (w *GarbageCollectionWorker) watchdog() {
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
				w.metrics.RecordWorkerError(w.name, "gc", "health_check_failed")
			}
		}
	}
}