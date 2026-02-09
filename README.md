# Workers

A Go library for managing background workers with periodic execution, retry logic, and graceful shutdown.

- **Periodic execution** - Run jobs at configurable intervals
- **Retry logic** - Automatic retries with configurable attempts and delay
- **Backoff strategies** - Constant, Linear, and Exponential backoff
- **Timeout support** - Set max execution time per job
- **Graceful shutdown** - Handles SIGINT/SIGTERM signals properly
- **Structured logging** - Built-in slog integration
- **Panic recovery** - Workers continue even if one panics

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "time"
    
    "github.com/assaidy/workers"
)

func main() {
    logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
    
    wm := workers.NewWorkerManager(
        workers.WithLogger(logger),
    )
    
    wm.RegisterWorker(workers.NewWorker("cleanup", cleanupJob,
        workers.WithTick(30*time.Minute),
        workers.WithTimeout(5*time.Minute),
        workers.WithRetries(3),
        workers.WithRetryDelay(5*time.Second),
    ))
    
    wm.RegisterWorker(workers.NewWorker("sync", syncJob,
        workers.WithTick(1*time.Minute),
        workers.WithTimeout(30*time.Second),
        workers.WithRetries(5),
        workers.WithRetryDelay(10*time.Second),
        workers.WithBackoffStrategy(workers.ExponentialBackoff),
    ))
    
    wm.Start()
}

func cleanupJob(ctx context.Context, log *slog.Logger) error {
    log.Info("running cleanup job")
    return nil
}

func syncJob(ctx context.Context, log *slog.Logger) error {
    log.Info("syncing data with external service")
    return fmt.Errorf("connection refused")
}
```
