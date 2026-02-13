// Package workers provides background job management with periodic execution,
// scheduling, retry logic, and graceful shutdown.
//
// Workers is designed for running background tasks like data processing,
// cleanup jobs, API synchronization, and scheduled reports.
//
// # Basic Usage
//
// Create a WorkerManager, register workers with their jobs, and start:
//
//	wm := workers.NewWorkerManager()
//	wm.RegisterWorker(workers.NewWorker("cleanup", cleanupJob))
//	wm.Start()
//
// Jobs are functions that receive a context and logger:
//
//	func cleanupJob(ctx context.Context, log *slog.Logger) error {
//	    log.Info("running cleanup")
//	    return nil
//	}
//
// # Configuration
//
// Configure workers using functional options:
//
//	workers.NewWorker("sync", syncJob,
//	    workers.WithTick(5*time.Minute),
//	    workers.WithTimeout(30*time.Second),
//	    workers.WithNRetries(3),
//	)
//
// # Scheduling
//
// Run jobs at specific times instead of intervals:
//
//	workers.WithSchedules(
//	    workers.DailyAt(9, 0),
//	    workers.WeeklyAt(time.Monday, 14, 30),
//	)
//
// # Retry and Backoff
//
// Automatic retries with configurable backoff strategies:
//
//	workers.WithNRetries(5),
//	workers.WithRetryDelay(10*time.Second),
//	workers.WithBackoffStrategy(workers.ExponentialBackoff)
//
// Available strategies: ConstantBackoff, LinearBackoff, ExponentialBackoff.
// Custom strategies can be defined as: func(time.Duration, int) time.Duration
//
// # Graceful Shutdown
//
// The library handles SIGINT and SIGTERM signals, allowing running jobs to
// complete before shutdown.
package workers
