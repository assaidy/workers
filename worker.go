package workers

import (
	"context"
	"log/slog"
	"math"
	"math/rand"
	"time"
)

// Worker represents a background job that executes periodically.
type Worker struct {
	name            string
	job             WorkerJob
	tick            time.Duration
	timeout         time.Duration
	retries         uint
	retryDelay      time.Duration
	backoffStrategy BackoffStrategy
	logger          *slog.Logger
}

// WorkerJob is a function that performs the actual work for a worker.
type WorkerJob func(context.Context, *slog.Logger) error

// BackoffStrategy calculates the delay duration before the next retry attempt.
// The attempt parameter is 1-based (first retry is attempt 1).
type BackoffStrategy func(baseDelay time.Duration, attempt uint) time.Duration

// Predefined backoff strategies implement various retry delay patterns.
// All strategies are type-safe and will cause compile-time errors if the
// [BackoffStrategy] signature changes.
//
// Strategy types:
//   - Fixed: Constant delay between retries
//   - Linear/Exponential: Increasing delay based on attempt number
//   - TODO: jittered backoff
var (
	// ConstantBackoff returns a fixed delay regardless of attempt number.
	// Example: 5s, 5s, 5s, 5s...
	ConstantBackoff BackoffStrategy = func(baseDelay time.Duration, attempt uint) time.Duration {
		return baseDelay
	}

	// LinearBackoff increases delay linearly with each attempt.
	// Formula: delay * attempt
	//
	// Example: 5s, 10s, 15s, 20s...
	LinearBackoff BackoffStrategy = func(baseDelay time.Duration, attempt uint) time.Duration {
		return baseDelay * time.Duration(attempt)
	}

	// ExponentialBackoff doubles the delay with each attempt.
	// Formula: delay * 2^attempt
	// Example: 5s, 10s, 20s, 40s...
	ExponentialBackoff BackoffStrategy = func(baseDelay time.Duration, attempt uint) time.Duration {
		return baseDelay * time.Duration(math.Pow(2, float64(attempt)))
	}
)

// NewWorker creates a new Worker with the given name and job function.
// Panics if invalid options are set.
func NewWorker(name string, job WorkerJob, options ...WorkerOption) Worker {
	w := Worker{
		name:            name,
		job:             job,
		tick:            1 * time.Hour,
		retries:         3,
		retryDelay:      5 * time.Second,
		backoffStrategy: ConstantBackoff,
	}

	for _, opt := range options {
		opt(&w)
	}

	if w.tick <= 0 {
		panic("tick must be > 0")
	}
	if w.timeout <= 0 {
		panic("timeout must be > 0")
	}
	if w.retryDelay <= 0 {
		panic("delay must be > 0")
	}

	return w
}

// WorkerOption is a function that configures a Worker.
type WorkerOption func(*Worker)

// WithItsOwnLogger sets a custom logger for the worker.
// Mustn't be nil.
//
// Default: logger of WorkerManager
func WithItsOwnLogger(logger *slog.Logger) WorkerOption {
	return func(w *Worker) {
		if logger == nil {
			panic("cannot explicitly set a nil logger")
		}
		w.logger = logger
	}
}

// WithTick sets the interval between job executions.
// Must be > 0.
//
// Default: 1 hour
func WithTick(tick time.Duration) WorkerOption {
	return func(w *Worker) {
		w.tick = tick
	}
}

// WithTimeout sets the maximum duration allowed for each job execution.
// Must be > 0.
//
// Default: no timeout
func WithTimeout(timeout time.Duration) WorkerOption {
	return func(w *Worker) {
		w.timeout = timeout
	}
}

// WithRetries sets the number of retry attempts for failed jobs.
//
// Default: 3 retries
func WithRetries(n uint) WorkerOption {
	return func(w *Worker) {
		w.retries = n
	}
}

// WithRetryDelay sets the delay between retry attempts.
// Must be > 0.
//
// Default: 5 seconds
func WithRetryDelay(delay time.Duration) WorkerOption {
	return func(w *Worker) {
		w.retryDelay = delay
	}
}

// WithBackoffStrategy sets the backoff strategy.
//
// Default: [ConstantBackoff]
func WithBackoffStrategy(strategy BackoffStrategy) WorkerOption {
	return func(w *Worker) {
		w.backoffStrategy = strategy
	}
}

// start begins the worker's execution loop.
func (me Worker) start(workerCtx context.Context) {
	me.logger.Info("worker started", "worker", me.name)
	defer me.logger.Info("worker stopped", "worker", me.name)

	ticker := time.NewTicker(me.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			me.logger.Info("job started", "worker", me.name)

			jobCtx, jobCtxCancel := context.WithTimeout(context.Background(), me.timeout)
			if err := me.job(jobCtx, me.logger); err != nil {
				me.logger.Error("job failed", "worker", me.name, "error", err)

				delay := me.retryDelay
				for i := uint(1); i <= me.retries; i++ {
					<-time.After(delay)
					me.logger.Error("job retry started", "worker", me.name, "retry", i)

					retryCtx, retryCtxCancel := context.WithTimeout(context.Background(), me.timeout)
					if err := me.job(retryCtx, me.logger); err != nil {
						me.logger.Error("job retry failed", "worker", me.name, "retry", i)
						retryCtxCancel()
						delay = me.backoffStrategy(me.retryDelay, i)
						continue
					}
					retryCtxCancel()

					me.logger.Error("job retry finished successfully", "worker", me.name, "retry", i)
					break
				}
			} else {
				me.logger.Info("job finished successfully", "worker", me.name)
			}
			jobCtxCancel()

		case <-workerCtx.Done():
			return
		}
	}
}
