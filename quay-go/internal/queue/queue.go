package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// QueueItem represents a work item in the queue
type QueueItem struct {
	ID                int        `json:"id" db:"id"`
	QueueName         string     `json:"queue_name" db:"queue_name"`
	Body              string     `json:"body" db:"body"`
	AvailableAfter    time.Time  `json:"available_after" db:"available_after"`
	Available         bool       `json:"available" db:"available"`
	ProcessingExpires *time.Time `json:"processing_expires" db:"processing_expires"`
	RetriesRemaining  int        `json:"retries_remaining" db:"retries_remaining"`
	StateID           string     `json:"state_id" db:"state_id"`
}

// Queue provides queue operations
type Queue struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewQueue creates a new queue instance
func NewQueue(db *sql.DB, logger *zap.Logger) *Queue {
	return &Queue{
		db:     db,
		logger: logger,
	}
}

// GetNextItem retrieves and claims the next available queue item
func (q *Queue) GetNextItem(ctx context.Context, queueName string, timeout time.Duration) (*QueueItem, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	
	// Find next available item
	query := `
		SELECT id, queue_name, body, available_after, available, 
		       processing_expires, retries_remaining, state_id
		FROM queueitem 
		WHERE queue_name = $1 
		  AND available = true 
		  AND available_after <= NOW() 
		  AND retries_remaining > 0
		  AND (processing_expires IS NULL OR processing_expires <= NOW())
		ORDER BY available_after ASC 
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`
	
	var item QueueItem
	err = tx.QueryRowContext(ctx, query, queueName).Scan(
		&item.ID, &item.QueueName, &item.Body, &item.AvailableAfter,
		&item.Available, &item.ProcessingExpires, &item.RetriesRemaining, &item.StateID,
	)
	
	if err == sql.ErrNoRows {
		return nil, nil // No items available
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query queue item: %w", err)
	}
	
	// Claim the item by marking it as unavailable and setting processing expires
	newStateID := generateStateID()
	processingExpires := time.Now().Add(timeout)
	
	updateQuery := `
		UPDATE queueitem 
		SET available = false, 
		    processing_expires = $1, 
		    state_id = $2
		WHERE id = $3 AND state_id = $4
	`
	
	result, err := tx.ExecContext(ctx, updateQuery, processingExpires, newStateID, item.ID, item.StateID)
	if err != nil {
		return nil, fmt.Errorf("failed to claim queue item: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		// Item was claimed by another worker
		return nil, nil
	}
	
	// Update the item with new state
	item.Available = false
	item.ProcessingExpires = &processingExpires
	item.StateID = newStateID
	
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	
	return &item, nil
}

// CompleteItem marks a queue item as completed and removes it
func (q *Queue) CompleteItem(ctx context.Context, item *QueueItem) error {
	query := `DELETE FROM queueitem WHERE id = $1`
	result, err := q.db.ExecContext(ctx, query, item.ID)
	if err != nil {
		return fmt.Errorf("failed to delete completed queue item: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		q.logger.Warn("Queue item not found when completing", zap.Int("item_id", item.ID))
	}
	
	return nil
}

// RetryItem marks a queue item for retry with exponential backoff
func (q *Queue) RetryItem(ctx context.Context, item *QueueItem, maxRetries int, err error) error {
	item.RetriesRemaining--
	
	if item.RetriesRemaining <= 0 {
		q.logger.Info("Queue item exceeded max retries, deleting",
			zap.String("state_id", item.StateID),
			zap.Int("max_retries", maxRetries),
			zap.Error(err))
		return q.CompleteItem(ctx, item)
	}
	
	// Calculate exponential backoff delay
	backoffDelay := time.Duration(maxRetries-item.RetriesRemaining) * time.Minute * 5
	availableAfter := time.Now().Add(backoffDelay)
	
	query := `
		UPDATE queueitem 
		SET available = true, 
		    processing_expires = NULL, 
		    retries_remaining = $1, 
		    available_after = $2,
		    state_id = $3
		WHERE id = $4
	`
	
	newStateID := generateStateID()
	result, err := q.db.ExecContext(ctx, query, item.RetriesRemaining, availableAfter, newStateID, item.ID)
	if err != nil {
		return fmt.Errorf("failed to update failed queue item: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return fmt.Errorf("queue item not found when retrying: %d", item.ID)
	}
	
	q.logger.Info("Queue item will retry",
		zap.String("state_id", item.StateID),
		zap.Duration("backoff_delay", backoffDelay),
		zap.Int("retries_remaining", item.RetriesRemaining))
	
	return nil
}

// AddItem adds a new item to the queue
func (q *Queue) AddItem(ctx context.Context, queueName string, body interface{}) error {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal queue item body: %w", err)
	}
	
	query := `
		INSERT INTO queueitem (queue_name, body, available_after, available, retries_remaining, state_id)
		VALUES ($1, $2, NOW(), true, 5, $3)
	`
	
	stateID := generateStateID()
	_, err = q.db.ExecContext(ctx, query, queueName, string(bodyBytes), stateID)
	if err != nil {
		return fmt.Errorf("failed to insert queue item: %w", err)
	}
	
	q.logger.Debug("Added item to queue",
		zap.String("queue_name", queueName),
		zap.String("state_id", stateID))
	
	return nil
}

// GetQueueStats returns statistics about a queue
func (q *Queue) GetQueueStats(ctx context.Context, queueName string) (QueueStats, error) {
	query := `
		SELECT 
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE available = true AND available_after <= NOW() AND retries_remaining > 0) as available,
			COUNT(*) FILTER (WHERE available = false) as processing,
			COUNT(*) FILTER (WHERE processing_expires < NOW()) as expired,
			COUNT(*) FILTER (WHERE retries_remaining = 0) as failed
		FROM queueitem 
		WHERE queue_name = $1
	`
	
	var stats QueueStats
	err := q.db.QueryRowContext(ctx, query, queueName).Scan(
		&stats.Total, &stats.Available, &stats.Processing, &stats.Expired, &stats.Failed)
	if err != nil {
		return stats, fmt.Errorf("failed to get queue stats: %w", err)
	}
	
	stats.QueueName = queueName
	return stats, nil
}

// QueueStats represents queue statistics
type QueueStats struct {
	QueueName  string `json:"queue_name"`
	Total      int    `json:"total"`
	Available  int    `json:"available"`
	Processing int    `json:"processing"`
	Expired    int    `json:"expired"`
	Failed     int    `json:"failed"`
}

// generateStateID generates a unique state ID for queue items
func generateStateID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix())
}