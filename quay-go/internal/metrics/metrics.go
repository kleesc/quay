package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/quay/quay-go/internal/config"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	// Worker metrics
	WorkersActive           prometheus.Gauge
	WorkerProcessed         *prometheus.CounterVec
	WorkerErrors            *prometheus.CounterVec
	WorkerDuration          *prometheus.HistogramVec
	
	// Queue metrics
	QueueItemsTotal         *prometheus.GaugeVec
	QueueItemsAvailable     *prometheus.GaugeVec
	QueueItemsProcessing    *prometheus.GaugeVec
	QueueItemsExpired       *prometheus.GaugeVec
	QueueItemsFailed        *prometheus.GaugeVec
	
	// GC metrics
	NamespacesDeleted       prometheus.Counter
	RepositoriesDeleted     prometheus.Counter
	
	// Database metrics
	DatabaseConnections     *prometheus.GaugeVec
	DatabaseQueries         *prometheus.CounterVec
	DatabaseQueryDuration   *prometheus.HistogramVec
	
	logger *zap.Logger
	server *http.Server
}

// NewMetrics creates a new metrics instance
func NewMetrics(cfg *config.MetricsConfig, logger *zap.Logger) *Metrics {
	namespace := cfg.Namespace
	
	m := &Metrics{
		logger: logger,
		
		// Worker metrics
		WorkersActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "workers_active_total",
			Help:      "Number of active workers",
		}),
		
		WorkerProcessed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "worker_items_processed_total",
			Help:      "Total number of items processed by workers",
		}, []string{"worker_type", "queue_name"}),
		
		WorkerErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "worker_errors_total",
			Help:      "Total number of worker errors",
		}, []string{"worker_type", "queue_name", "error_type"}),
		
		WorkerDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "worker_processing_duration_seconds",
			Help:      "Time spent processing items",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 15), // 0.1s to ~54m
		}, []string{"worker_type", "queue_name"}),
		
		// Queue metrics
		QueueItemsTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_items_total",
			Help:      "Total number of items in queue",
		}, []string{"queue_name"}),
		
		QueueItemsAvailable: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_items_available",
			Help:      "Number of available items in queue",
		}, []string{"queue_name"}),
		
		QueueItemsProcessing: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_items_processing",
			Help:      "Number of items currently being processed",
		}, []string{"queue_name"}),
		
		QueueItemsExpired: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_items_expired",
			Help:      "Number of expired items in queue",
		}, []string{"queue_name"}),
		
		QueueItemsFailed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_items_failed",
			Help:      "Number of failed items in queue",
		}, []string{"queue_name"}),
		
		// GC metrics
		NamespacesDeleted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "namespaces_deleted_total",
			Help:      "Total number of namespaces deleted",
		}),
		
		RepositoriesDeleted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "repositories_deleted_total",
			Help:      "Total number of repositories deleted",
		}),
		
		// Database metrics
		DatabaseConnections: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "database_connections",
			Help:      "Number of database connections",
		}, []string{"state"}), // open, idle, in_use
		
		DatabaseQueries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "database_queries_total",
			Help:      "Total number of database queries",
		}, []string{"operation"}),
		
		DatabaseQueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "database_query_duration_seconds",
			Help:      "Time spent on database queries",
			Buckets:   prometheus.DefBuckets,
		}, []string{"operation"}),
	}
	
	// Register all metrics
	prometheus.MustRegister(
		m.WorkersActive,
		m.WorkerProcessed,
		m.WorkerErrors,
		m.WorkerDuration,
		m.QueueItemsTotal,
		m.QueueItemsAvailable,
		m.QueueItemsProcessing,
		m.QueueItemsExpired,
		m.QueueItemsFailed,
		m.NamespacesDeleted,
		m.RepositoriesDeleted,
		m.DatabaseConnections,
		m.DatabaseQueries,
		m.DatabaseQueryDuration,
	)
	
	return m
}

// StartServer starts the metrics HTTP server
func (m *Metrics) StartServer(cfg *config.MetricsConfig) error {
	if !cfg.Enabled {
		m.logger.Info("Metrics server disabled")
		return nil
	}
	
	mux := http.NewServeMux()
	mux.Handle(cfg.Path, promhttp.Handler())
	
	// Add health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	
	addr := fmt.Sprintf(":%d", cfg.Port)
	m.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	
	m.logger.Info("Starting metrics server",
		zap.String("addr", addr),
		zap.String("path", cfg.Path))
	
	go func() {
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			m.logger.Error("Metrics server failed", zap.Error(err))
		}
	}()
	
	return nil
}

// Shutdown gracefully shuts down the metrics server
func (m *Metrics) Shutdown(ctx context.Context) error {
	if m.server == nil {
		return nil
	}
	
	m.logger.Info("Shutting down metrics server")
	return m.server.Shutdown(ctx)
}

// RecordWorkerProcessed records a processed item
func (m *Metrics) RecordWorkerProcessed(workerType, queueName string) {
	m.WorkerProcessed.WithLabelValues(workerType, queueName).Inc()
}

// RecordWorkerError records a worker error
func (m *Metrics) RecordWorkerError(workerType, queueName, errorType string) {
	m.WorkerErrors.WithLabelValues(workerType, queueName, errorType).Inc()
}

// RecordWorkerDuration records worker processing duration
func (m *Metrics) RecordWorkerDuration(workerType, queueName string, duration time.Duration) {
	m.WorkerDuration.WithLabelValues(workerType, queueName).Observe(duration.Seconds())
}

// UpdateQueueStats updates queue statistics
func (m *Metrics) UpdateQueueStats(queueName string, total, available, processing, expired, failed int) {
	m.QueueItemsTotal.WithLabelValues(queueName).Set(float64(total))
	m.QueueItemsAvailable.WithLabelValues(queueName).Set(float64(available))
	m.QueueItemsProcessing.WithLabelValues(queueName).Set(float64(processing))
	m.QueueItemsExpired.WithLabelValues(queueName).Set(float64(expired))
	m.QueueItemsFailed.WithLabelValues(queueName).Set(float64(failed))
}

// RecordNamespaceDeleted records a deleted namespace
func (m *Metrics) RecordNamespaceDeleted() {
	m.NamespacesDeleted.Inc()
}

// RecordRepositoryDeleted records a deleted repository
func (m *Metrics) RecordRepositoryDeleted() {
	m.RepositoriesDeleted.Inc()
}

// RecordRepositoryGC records a repository garbage collection
func (m *Metrics) RecordRepositoryGC(repositoryName string) {
	// We can use the worker processed metric for this
	m.WorkerProcessed.WithLabelValues("garbage-collection", repositoryName).Inc()
}

// UpdateDatabaseConnections updates database connection metrics
func (m *Metrics) UpdateDatabaseConnections(open, idle, inUse int) {
	m.DatabaseConnections.WithLabelValues("open").Set(float64(open))
	m.DatabaseConnections.WithLabelValues("idle").Set(float64(idle))
	m.DatabaseConnections.WithLabelValues("in_use").Set(float64(inUse))
}

// RecordDatabaseQuery records a database query
func (m *Metrics) RecordDatabaseQuery(operation string, duration time.Duration) {
	m.DatabaseQueries.WithLabelValues(operation).Inc()
	m.DatabaseQueryDuration.WithLabelValues(operation).Observe(duration.Seconds())
}