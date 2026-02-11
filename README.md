# Workers

A Go library for managing background workers with periodic execution, retry logic, and graceful shutdown.

- **Periodic execution** - Run jobs at configurable intervals or specific times
- **Scheduling** - Daily, weekly, or every N days at specific times
- **Retry logic** - Automatic retries with configurable attempts and delay
- **Backoff strategies** - Constant, Linear, and Exponential backoff
- **Timeout support** - Set max execution time per job
- **Execution limit** - Limit total number of job runs
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
    
    // Schedule jobs at specific times
    wm.RegisterWorker(workers.NewWorker("report", reportJob,
        workers.WithSchedules(
            workers.DailyAt(9, 0),                 // Daily at 9:00 AM
            workers.WeeklyAt(time.Monday, 14, 30), // Every Monday at 2:30 PM
        ),
        workers.WithTimeout(10*time.Minute),
    ))

    // Limit total executions
    wm.RegisterWorker(workers.NewWorker("migrate", migrateJob,
        workers.WithTick(5*time.Minute),
        workers.WithNRuns(10), // Run 10 times then stop
        workers.WithTimeout(2*time.Minute),
    ))

    // Run once without timeout (job might hang)
    wm.RegisterWorker(workers.NewWorker("long-task", longRunningJob,
        workers.WithNRuns(1), // Run once and stop
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

func reportJob(ctx context.Context, log *slog.Logger) error {
    log.Info("generating daily report")
    return nil
}

func migrateJob(ctx context.Context, log *slog.Logger) error {
    log.Info("running migration batch")
    return nil
}

func longRunningJob(ctx context.Context, log *slog.Logger) error {
    log.Info("starting long-running task without timeout")
    // This job could potentially hang forever
    // The worker will complete after this single run
    select {}
}
```
