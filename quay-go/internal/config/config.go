package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config represents the application configuration
type Config struct {
	// Database configuration
	Database DatabaseConfig `mapstructure:"database"`
	
	// Redis configuration for locking and caching
	Redis RedisConfig `mapstructure:"redis"`
	
	// Queue configuration
	Queue QueueConfig `mapstructure:"queue"`
	
	// Worker configuration
	Workers WorkerConfig `mapstructure:"workers"`
	
	// Metrics configuration
	Metrics MetricsConfig `mapstructure:"metrics"`
	
	// Logging configuration
	Logging LoggingConfig `mapstructure:"logging"`
	
	// Feature flags
	Features FeatureConfig `mapstructure:"features"`
}

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Name            string        `mapstructure:"name"`
	SSLMode         string        `mapstructure:"ssl_mode"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
}

// RedisConfig holds Redis connection settings
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// QueueConfig holds queue-related settings
type QueueConfig struct {
	NamespaceGC  QueueSettings `mapstructure:"namespace_gc"`
	RepositoryGC QueueSettings `mapstructure:"repository_gc"`
}

// QueueSettings holds individual queue settings
type QueueSettings struct {
	Name               string        `mapstructure:"name"`
	PollPeriod         time.Duration `mapstructure:"poll_period"`
	Timeout            time.Duration `mapstructure:"timeout"`
	LockTimeoutPadding time.Duration `mapstructure:"lock_timeout_padding"`
	MaxRetries         int           `mapstructure:"max_retries"`
	ChunkSize          int           `mapstructure:"chunk_size"`
}

// WorkerConfig holds worker-specific settings
type WorkerConfig struct {
	WatchdogPeriod   time.Duration `mapstructure:"watchdog_period"`
	MetricsInterval  time.Duration `mapstructure:"metrics_interval"`
	ShutdownTimeout  time.Duration `mapstructure:"shutdown_timeout"`
	MaxWorkers       int           `mapstructure:"max_workers"`
	GCFrequency      int           `mapstructure:"gc_frequency"`
}

// MetricsConfig holds metrics and monitoring settings
type MetricsConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Port      int    `mapstructure:"port"`
	Path      string `mapstructure:"path"`
	Namespace string `mapstructure:"namespace"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"` // json or console
}

// FeatureConfig holds feature flags
type FeatureConfig struct {
	NamespaceGarbageCollection  bool `mapstructure:"namespace_garbage_collection"`
	RepositoryGarbageCollection bool `mapstructure:"repository_garbage_collection"`
	GarbageCollection           bool `mapstructure:"garbage_collection"`
	ImageExpiryTrigger          bool `mapstructure:"image_expiry_trigger"`
	QuotaManagement             bool `mapstructure:"quota_management"`
	SecurityScanning            bool `mapstructure:"security_scanning"`
}

// DSN returns the PostgreSQL connection string
func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode)
}

// RedisAddr returns the Redis connection address
func (c *RedisConfig) RedisAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// LoadConfig loads configuration from various sources
func LoadConfig(configPath string) (*Config, error) {
	v := viper.New()
	
	// Set defaults
	setDefaults(v)
	
	// Set config file path if provided
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		// Search for config in common locations
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/quay-go")
	}
	
	// Read environment variables
	v.SetEnvPrefix("QUAY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	
	// Read config file
	if err := v.ReadInConfig(); err != nil {
		// If config file is not found, that's OK - we'll use defaults and env vars
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}
	
	// Unmarshal into struct
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}
	
	return &config, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// Database defaults
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "quay")
	v.SetDefault("database.password", "")
	v.SetDefault("database.name", "quay")
	v.SetDefault("database.ssl_mode", "disable")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", "5m")
	v.SetDefault("database.conn_max_idle_time", "5m")
	
	// Redis defaults
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	
	// Queue defaults
	v.SetDefault("queue.namespace_gc.name", "namespace_gc")
	v.SetDefault("queue.namespace_gc.poll_period", "60s")
	v.SetDefault("queue.namespace_gc.timeout", "3h")
	v.SetDefault("queue.namespace_gc.lock_timeout_padding", "60s")
	v.SetDefault("queue.namespace_gc.max_retries", 5)
	v.SetDefault("queue.namespace_gc.chunk_size", 100)
	
	v.SetDefault("queue.repository_gc.name", "repository_gc")
	v.SetDefault("queue.repository_gc.poll_period", "60s")
	v.SetDefault("queue.repository_gc.timeout", "3h")
	v.SetDefault("queue.repository_gc.lock_timeout_padding", "60s")
	v.SetDefault("queue.repository_gc.max_retries", 5)
	v.SetDefault("queue.repository_gc.chunk_size", 100)
	
	// Worker defaults
	v.SetDefault("workers.watchdog_period", "60s")
	v.SetDefault("workers.metrics_interval", "30s")
	v.SetDefault("workers.shutdown_timeout", "30s")
	v.SetDefault("workers.max_workers", 1)
	
	// Metrics defaults
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.port", 8080)
	v.SetDefault("metrics.path", "/metrics")
	v.SetDefault("metrics.namespace", "quay")
	
	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "console")
	
	// Feature defaults
	v.SetDefault("features.namespace_garbage_collection", true)
	v.SetDefault("features.repository_garbage_collection", true)
	v.SetDefault("features.quota_management", false)
	v.SetDefault("features.security_scanning", false)
}