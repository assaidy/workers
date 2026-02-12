# Workers

A Go library for managing background workers with periodic execution, retry logic, and graceful shutdown.

## Features

- **Periodic execution** - Run jobs at configurable intervals or specific times
- **Scheduling** - Daily, weekly, or every N days at specific times
- **Retry logic** - Automatic retries with configurable attempts and delay
- **Backoff strategies** - Constant, Linear, and Exponential backoff
- **Timeout support** - Set max execution time per job
- **Execution limit** - Limit total number of job runs
- **Graceful shutdown** - Handles SIGINT/SIGTERM signals properly
- **Structured logging** - Built-in slog integration
- **Panic recovery** - Workers continue even if one panics

## Installation

```bash
go get github.com/assaidy/workers
```

## Quick Start

```go
package main

import (
    "context"
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
        workers.WithNRetries(3),
        workers.WithRetryDelay(5*time.Second),
    ))
    
    wm.Start()
}

func cleanupJob(ctx context.Context, log *slog.Logger) error {
    log.Info("running cleanup job")
    return nil
}
```

## Use Cases

### 1. Basic Periodic Worker

Run a job at fixed time intervals.

```go
wm.RegisterWorker(workers.NewWorker("metrics", metricsJob,
    workers.WithTick(1*time.Minute),     // Run every minute
    workers.WithTimeout(30*time.Second), // Max execution time
))
```

**When to use:** Data collection, health checks, cache warming.

### 2. Daily Scheduled Tasks

Run jobs at specific times of the day.

```go
wm.RegisterWorker(workers.NewWorker("report", reportJob,
    workers.WithSchedules(
        workers.DailyAt(9, 0),   // Daily at 9:00 AM
        workers.DailyAt(17, 0),  // Daily at 5:00 PM
    ),
    workers.WithTimeout(10*time.Minute),
))
```

**When to use:** Daily reports, backups, maintenance windows.

### 3. Weekly Scheduled Tasks

Run jobs on specific days of the week.

```go
wm.RegisterWorker(workers.NewWorker("weekly-sync", weeklySyncJob,
    workers.WithSchedules(
        workers.WeeklyAt(time.Monday, 8, 0),    // Every Monday at 8:00 AM
        workers.WeeklyAt(time.Friday, 16, 30),  // Every Friday at 4:30 PM
    ),
    workers.WithTimeout(2*time.Hour),
))
```

**When to use:** Weekly summaries, end-of-week processing, weekend maintenance.

### 4. Every N Days

Run jobs on custom intervals.

```go
wm.RegisterWorker(workers.NewWorker("bi-daily", biDailyJob,
    workers.WithSchedules(
        workers.EveryNDays(2, 2, 0),   // Every 2 days at 2:00 AM
    ),
    workers.WithTimeout(1*time.Hour),
))
```

**When to use:** Bi-daily processing, custom business cycles.

### 5. Retry with Backoff Strategies

Handle failures gracefully with automatic retries.

```go
// Constant backoff: 5s, 5s, 5s, 5s...
wm.RegisterWorker(workers.NewWorker("api-sync", apiSyncJob,
    workers.WithTick(5*time.Minute),
    workers.WithNRetries(5),
    workers.WithRetryDelay(5*time.Second),
    workers.WithBackoffStrategy(workers.ConstantBackoff),
))

// Linear backoff: 5s, 10s, 15s, 20s...
wm.RegisterWorker(workers.NewWorker("slow-api", slowApiJob,
    workers.WithTick(10*time.Minute),
    workers.WithNRetries(4),
    workers.WithRetryDelay(5*time.Second),
    workers.WithBackoffStrategy(workers.LinearBackoff),
))

// Exponential backoff: 5s, 10s, 20s, 40s...
wm.RegisterWorker(workers.NewWorker("unstable-api", unstableApiJob,
    workers.WithTick(15*time.Minute),
    workers.WithNRetries(3),
    workers.WithRetryDelay(5*time.Second),
    workers.WithBackoffStrategy(workers.ExponentialBackoff),
))
```

**When to use:** API integrations, external service calls, flaky network operations.

### 6. Limited Execution Count

Run a job a fixed number of times then stop.

```go
wm.RegisterWorker(workers.NewWorker("migration", migrationJob,
    workers.WithTick(1*time.Minute),
    workers.WithNRuns(10),  // Run exactly 10 times
    workers.WithTimeout(2*time.Minute),
))
```

**When to use:** Data migrations, one-time setup tasks, batch processing with known size.

### 7. Run Once Without Timeout

Execute a job once without any time limits.

```go
wm.RegisterWorker(workers.NewWorker("long-task", longRunningJob,
    workers.WithNRuns(1),  // Run once and stop
))
```

**When to use:** Long-running initialization, unbounded processing tasks.

### 8. Custom Logger Per Worker

Override the default logger for specific workers.

> **Note:** The `*slog.Logger` passed as the second parameter to your job function is the worker's logger (inherited from WorkerManager unless overridden with `WithItsOwnLogger`).

```go
customLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

wm.RegisterWorker(workers.NewWorker("json-logger", jobFunc,
    workers.WithTick(5*time.Minute),
    workers.WithItsOwnLogger(customLogger),
))
```

**When to use:** Different log formats per worker, separate log destinations, custom log levels.

### 9. Timezone Support

Schedule jobs in specific timezones.

```go
est := time.FixedZone("EST", -5*60*60)
utc := time.UTC

wm.RegisterWorker(workers.NewWorker("ny-report", reportJob,
    workers.WithSchedules(
        workers.DailyAt(9, 0).In(est),  // 9:00 AM EST
    ),
))

wm.RegisterWorker(workers.NewWorker("utc-cleanup", cleanupJob,
    workers.WithSchedules(
        workers.DailyAt(0, 0).In(utc),  // Midnight UTC
    ),
))
```

**When to use:** Multi-region applications, coordinating with teams in different timezones.

## Configuration Options

### WorkerManager Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithLogger(logger)` | Set logger for all workers | `slog.Default()` |

### Worker Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithTick(duration)` | Interval between executions | 1 hour |
| `WithTimeout(duration)` | Max execution time per job | No timeout |
| `WithNRetries(n)` | Number of retry attempts | 3 |
| `WithRetryDelay(duration)` | Delay between retries | 5 seconds |
| `WithBackoffStrategy(strategy)` | Retry backoff pattern | `ConstantBackoff` |
| `WithSchedules(schedules...)` | Specific run times | Tick-based |
| `WithNRuns(n)` | Limit total executions | Unlimited |
| `WithItsOwnLogger(logger)` | Worker-specific logger | Manager's logger |

### Schedule Functions

| Function | Description |
|----------|-------------|
| `DailyAt(hour, minute)` | Run daily at specified time |
| `WeeklyAt(weekday, hour, minute)` | Run weekly on specified day |
| `EveryNDays(n, hour, minute)` | Run every N days |
| `schedule.In(location)` | Set timezone for schedule |

### Backoff Strategies

| Strategy | Pattern | Example (5s base) |
|----------|---------|-------------------|
| `ConstantBackoff` | Fixed delay | 5s, 5s, 5s, 5s |
| `LinearBackoff` | Linear increase | 5s, 10s, 15s, 20s |
| `ExponentialBackoff` | Exponential increase | 5s, 10s, 20s, 40s |

**Custom backoff strategies:** You can define your own backoff strategy by implementing the `BackoffStrategy` type:

```go
type BackoffStrategy func(baseDelay time.Duration, attempt int) time.Duration

// Example: Custom backoff that increases by 3x each attempt
myBackoff := func(baseDelay time.Duration, attempt int) time.Duration {
    return baseDelay * time.Duration(math.Pow(3, float64(attempt)))
}
// 5s, 15s, 45s, 135s...

wm.RegisterWorker(workers.NewWorker("custom", jobFunc,
    workers.WithBackoffStrategy(myBackoff),
))
```

## Graceful Shutdown

The library handles SIGINT (Ctrl+C) and SIGTERM signals gracefully:

1. All running jobs complete their current execution
2. Workers stop accepting new jobs
3. Application exits cleanly

Press Ctrl+C twice to force immediate shutdown.

## Complete Example

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
    
    // 1. Periodic worker with retries
    wm.RegisterWorker(workers.NewWorker("cleanup", cleanupJob,
        workers.WithTick(30*time.Minute),
        workers.WithTimeout(5*time.Minute),
        workers.WithNRetries(3),
        workers.WithRetryDelay(5*time.Second),
    ))
    
    // 2. Daily scheduled reports
    wm.RegisterWorker(workers.NewWorker("report", reportJob,
        workers.WithSchedules(
            workers.DailyAt(9, 0),
            workers.WeeklyAt(time.Monday, 14, 30),
        ),
        workers.WithTimeout(10*time.Minute),
    ))
    
    // 3. Limited run migration
    wm.RegisterWorker(workers.NewWorker("migrate", migrateJob,
        workers.WithTick(5*time.Minute),
        workers.WithNRuns(10),
        workers.WithTimeout(2*time.Minute),
    ))
    
    // 4. One-time long task
    wm.RegisterWorker(workers.NewWorker("long-task", longRunningJob,
        workers.WithNRuns(1),
    ))
    
    wm.Start()
}

func cleanupJob(ctx context.Context, log *slog.Logger) error {
    log.Info("running cleanup job")
    return nil
}

func reportJob(ctx context.Context, log *slog.Logger) error {
    log.Info("generating report")
    return nil
}

func migrateJob(ctx context.Context, log *slog.Logger) error {
    log.Info("running migration")
    return nil
}

func longRunningJob(ctx context.Context, log *slog.Logger) error {
    log.Info("starting long task")
    select {}
}
```
