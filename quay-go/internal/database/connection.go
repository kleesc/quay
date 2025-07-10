package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	_ "github.com/lib/pq"
	"go.uber.org/zap"

	"github.com/quay/quay-go/internal/config"
)

// Connection holds database and Redis connections
type Connection struct {
	DB          *sql.DB
	RedisClient *redis.Client
	Logger      *zap.Logger
}

// NewConnection creates new database and Redis connections
func NewConnection(cfg *config.Config, logger *zap.Logger) (*Connection, error) {
	// Create PostgreSQL connection
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}
	
	// Configure connection pool
	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.Database.ConnMaxIdleTime)
	
	// Test database connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	
	logger.Info("Database connection established",
		zap.String("host", cfg.Database.Host),
		zap.Int("port", cfg.Database.Port),
		zap.String("database", cfg.Database.Name))
	
	// Create Redis connection
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.RedisAddr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	
	// Test Redis connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Warn("Redis connection failed, proceeding without distributed locking",
			zap.Error(err),
			zap.String("addr", cfg.Redis.RedisAddr()))
		redisClient.Close()
		redisClient = nil
	} else {
		logger.Info("Redis connection established",
			zap.String("addr", cfg.Redis.RedisAddr()),
			zap.Int("db", cfg.Redis.DB))
	}
	
	return &Connection{
		DB:          db,
		RedisClient: redisClient,
		Logger:      logger,
	}, nil
}

// Close closes database and Redis connections
func (c *Connection) Close() error {
	var dbErr, redisErr error
	
	if c.DB != nil {
		dbErr = c.DB.Close()
		if dbErr != nil {
			c.Logger.Error("Failed to close database connection", zap.Error(dbErr))
		} else {
			c.Logger.Info("Database connection closed")
		}
	}
	
	if c.RedisClient != nil {
		redisErr = c.RedisClient.Close()
		if redisErr != nil {
			c.Logger.Error("Failed to close Redis connection", zap.Error(redisErr))
		} else {
			c.Logger.Info("Redis connection closed")
		}
	}
	
	if dbErr != nil {
		return dbErr
	}
	return redisErr
}

// HealthCheck performs health checks on connections
func (c *Connection) HealthCheck(ctx context.Context) error {
	// Check database
	if err := c.DB.PingContext(ctx); err != nil {
		return fmt.Errorf("database health check failed: %w", err)
	}
	
	// Check Redis (if available)
	if c.RedisClient != nil {
		if err := c.RedisClient.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("redis health check failed: %w", err)
		}
	}
	
	return nil
}