# Quay Go Workers

This directory contains Go implementations of Quay workers, providing separate microservices for different garbage collection tasks.

## Workers

### 1. GC Worker (`gcworker`)

Based on `workers/gc/gcworker.py`, this worker handles general repository garbage collection:

- **Purpose**: Cleans up expired tags, manifests, and blobs in repositories
- **Features**:
  - Policy-based garbage collection
  - Repository selection with random distribution
  - Chunked processing for memory efficiency
  - Image expiry notifications
  - Prometheus metrics integration
- **Entry Point**: `cmd/gcworker/main.go`
- **Implementation**: `internal/gc/worker.go`

### 2. Namespace GC Worker (`namespacegcworker`)

Based on `workers/namespacegcworker.py`, this worker handles namespace deletion:

- **Purpose**: Cleans up deleted namespaces and their associated resources
- **Features**:
  - Queue-based processing
  - Comprehensive namespace deletion (repositories, teams, permissions, etc.)
  - Transactional safety with rollback support
  - Global locking for preventing concurrent operations
  - Prometheus metrics integration
- **Entry Point**: `cmd/namespacegcworker/main.go`
- **Implementation**: `internal/namespacegc/worker.go`

### 3. Repository GC Worker (`repositorygcworker`)

Based on `workers/repositorygcworker.py`, this worker handles repository deletion:

- **Purpose**: Cleans up deleted repositories and their associated resources
- **Features**:
  - Queue-based processing
  - Repository deletion with manifest/blob cleanup
  - Transactional safety with rollback support
  - Global locking for preventing concurrent operations
  - Prometheus metrics integration
- **Entry Point**: `cmd/repositorygcworker/main.go`
- **Implementation**: `internal/repositorygc/worker.go`

## Architecture

### Common Components

- **Configuration**: `internal/config/` - Viper-based configuration management
- **Database**: `internal/database/` - PostgreSQL connection handling and models
- **Queue**: `internal/queue/` - Queue operations for worker communication
- **Metrics**: `internal/metrics/` - Prometheus metrics collection
- **Shared Utilities**: Logging, health checks, graceful shutdown

### Key Features

- **Microservice Architecture**: Each worker runs as an independent service
- **Distributed Locking**: Redis-based locking to prevent concurrent operations
- **Queue Processing**: Optimistic locking with retry mechanisms
- **Metrics & Monitoring**: Prometheus metrics with Grafana dashboards
- **Health Checks**: HTTP endpoints for container orchestration
- **Graceful Shutdown**: Context-based cancellation with timeout handling

## Building

### Build All Workers

```bash
# Using the workers Makefile
make -f Makefile.workers build-all

# Or individually
make -f Makefile.workers build-gc
make -f Makefile.workers build-namespace-gc
make -f Makefile.workers build-repository-gc
```

### Build for Multiple Platforms

```bash
make -f Makefile.workers build-all-platforms
```

## Configuration

### Sample Configuration (`config.yaml`)

```yaml
# Database connection settings
database:
  host: localhost
  port: 5432
  user: quay
  password: password
  name: quay
  ssl_mode: disable
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: 5m
  conn_max_idle_time: 5m

# Redis connection settings (for distributed locking)
redis:
  host: localhost
  port: 6379
  password: ""
  db: 0

# Queue configuration
queue:
  namespace_gc:
    name: "namespace_gc"
    poll_period: 60s
    timeout: 3h
    lock_timeout_padding: 60s
    max_retries: 5
    chunk_size: 100
  repository_gc:
    name: "repository_gc"
    poll_period: 60s
    timeout: 3h
    lock_timeout_padding: 60s
    max_retries: 5
    chunk_size: 100

# Worker settings
workers:
  watchdog_period: 60s
  metrics_interval: 30s
  shutdown_timeout: 30s
  max_workers: 1
  gc_frequency: 30  # Garbage collection frequency in seconds

# Metrics and monitoring
metrics:
  enabled: true
  port: 8080
  path: "/metrics"
  namespace: "quay"

# Logging configuration
logging:
  level: info      # debug, info, warn, error
  format: console  # console, json

# Feature flags
features:
  namespace_garbage_collection: true
  repository_garbage_collection: true
  garbage_collection: true
  image_expiry_trigger: true
  quota_management: false
  security_scanning: false
```

## Running

### Standalone Execution

```bash
# GC Worker
./bin/gcworker --config config.yaml

# Namespace GC Worker
./bin/namespacegcworker --config config.yaml

# Repository GC Worker
./bin/repositorygcworker --config config.yaml
```

### Using Make Targets

```bash
make -f Makefile.workers run-gc
make -f Makefile.workers run-namespace-gc
make -f Makefile.workers run-repository-gc
```

### Docker Compose

```bash
# Start all workers with dependencies
docker-compose -f docker-compose.workers.yml up -d

# View logs
docker-compose -f docker-compose.workers.yml logs -f gcworker
docker-compose -f docker-compose.workers.yml logs -f namespacegcworker
docker-compose -f docker-compose.workers.yml logs -f repositorygcworker

# Stop all services
docker-compose -f docker-compose.workers.yml down
```

## Monitoring

### Metrics Endpoints

- **GC Worker**: http://localhost:8080/metrics
- **Namespace GC Worker**: http://localhost:8081/metrics (when using Docker Compose)
- **Repository GC Worker**: http://localhost:8082/metrics (when using Docker Compose)

### Prometheus

Access Prometheus at http://localhost:9090 to view metrics and create queries.

### Grafana

Access Grafana at http://localhost:3000 (admin/admin) for dashboard visualization.

### Key Metrics

- `quay_workers_active` - Number of active workers
- `quay_worker_processed_total` - Total items processed by workers
- `quay_worker_errors_total` - Total worker errors
- `quay_worker_duration_seconds` - Processing duration histogram
- `quay_queue_items_total` - Queue statistics
- `quay_database_connections` - Database connection metrics
- `quay_namespaces_deleted_total` - Deleted namespaces counter
- `quay_repositories_deleted_total` - Deleted repositories counter

## Development

### Prerequisites

- Go 1.21+
- PostgreSQL 12+
- Redis 6+
- Docker (for containerized deployment)

### Development Setup

```bash
# Install development tools
make -f Makefile.workers install-tools

# Run tests
make -f Makefile.workers test

# Run tests with coverage
make -f Makefile.workers test-coverage

# Format code
make -f Makefile.workers fmt

# Lint code
make -f Makefile.workers lint

# Complete development setup
make -f Makefile.workers dev-setup
```

### Database Schema

The workers expect the Quay database schema with the following key tables:

- `deletednamespace` - Namespace deletion markers
- `deletedrepository` - Repository deletion markers  
- `repository` - Repository definitions
- `tag` - Container image tags
- `manifest` - Image manifests
- `uploadedblob` - Blob uploads
- `namespace` - User/organization namespaces

## Deployment

### Kubernetes

Example Kubernetes deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: quay-gcworker
spec:
  replicas: 1
  selector:
    matchLabels:
      app: quay-gcworker
  template:
    metadata:
      labels:
        app: quay-gcworker
    spec:
      containers:
      - name: gcworker
        image: quay-gcworker:latest
        ports:
        - containerPort: 8080
        env:
        - name: QUAY_DATABASE_HOST
          value: "postgres-service"
        - name: QUAY_REDIS_HOST
          value: "redis-service"
        volumeMounts:
        - name: config
          mountPath: /app/config.yaml
          subPath: config.yaml
        livenessProbe:
          httpGet:
            path: /metrics
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /metrics
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
      volumes:
      - name: config
        configMap:
          name: quay-worker-config
```

### Scaling

Each worker type can be scaled independently:

- **GC Worker**: Can run multiple instances (they coordinate via Redis locks)
- **Namespace GC Worker**: Typically single instance (queue ensures no conflicts)
- **Repository GC Worker**: Typically single instance (queue ensures no conflicts)

## Troubleshooting

### Common Issues

1. **Database Connection Errors**
   - Check database connection parameters
   - Verify database is accessible
   - Check connection limits

2. **Redis Connection Errors**
   - Verify Redis is running and accessible
   - Check Redis configuration

3. **Queue Processing Stuck**
   - Check queue table for stuck items
   - Verify worker has proper database permissions
   - Check logs for processing errors

4. **High Memory Usage**
   - Adjust `chunk_size` in configuration
   - Monitor processing of large repositories

### Logs

All workers use structured logging with configurable levels:

```bash
# Debug level logging
./bin/gcworker --config config.yaml # with logging.level: debug in config

# JSON format logging
./bin/gcworker --config config.yaml # with logging.format: json in config
```

### Health Checks

Workers expose health information via metrics endpoint:

```bash
# Check worker health
curl http://localhost:8080/metrics
```

## License

This implementation follows the same license as the original Quay project.