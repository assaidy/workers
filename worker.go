package workers

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"
)

// Worker represents a background job that executes periodically.
type Worker struct {
	name            string
	job             WorkerJob
	tick            time.Duration
	schedules       []Schedule
	timeout         time.Duration
	nRetries        int
	retryDelay      time.Duration
	backoffStrategy BackoffStrategy
	nRuns           int
	runCount        int
	logger          *slog.Logger
}

// WorkerJob is a function that performs the actual work for a worker.
type WorkerJob func(context.Context, *slog.Logger) error

// Schedule represents a specific time pattern for running a worker.
// Use the constructor functions DailyAt, WeeklyAt, or EveryNDays to create schedules.
type Schedule struct {
	hour       int
	minute     int
	weekday    *time.Weekday
	interval   int
	anchorDate *time.Time // For EveryNDays: the reference date to calculate intervals from
	location   *time.Location
}

// DailyAt creates a schedule that runs daily at the specified hour and minute.
// Hour must be between 0-23 and minute must be between 0-59.
//
// Example:
//
//	workers.DailyAt(9, 0)    // Daily at 9:00 AM
//	workers.DailyAt(14, 30)  // Daily at 2:30 PM
func DailyAt(hour, minute int) Schedule {
	validateHour(hour)
	validateMinute(minute)
	return Schedule{
		hour:   hour,
		minute: minute,
	}
}

// WeeklyAt creates a schedule that runs weekly on a specific day at the specified time.
// Weekday specifies which day of the week (time.Monday through time.Sunday).
// Hour must be between 0-23 and minute must be between 0-59.
//
// Example:
//
//	workers.WeeklyAt(time.Monday, 9, 0)     // Every Monday at 9:00 AM
//	workers.WeeklyAt(time.Friday, 17, 30)   // Every Friday at 5:30 PM
func WeeklyAt(weekday time.Weekday, hour, minute int) Schedule {
	validateHour(hour)
	validateMinute(minute)
	return Schedule{
		hour:    hour,
		minute:  minute,
		weekday: &weekday,
	}
}

// EveryNDays creates a schedule that runs every N days at the specified time.
// The anchor date is set to today at midnight, so the pattern starts from today.
// For example, if created on Monday with n=2, it will run on Mon, Wed, Fri, Sun, etc.
// Hour must be between 0-23 and minute must be between 0-59.
//
// Example:
//
//	workers.EveryNDays(2, 9, 0)   // Every 2 days at 9:00 AM
//	workers.EveryNDays(7, 14, 0)  // Weekly at 2:00 PM - same as WeeklyAt(time.Now().Weekday(), hour, minute)
func EveryNDays(n, hour, minute int) Schedule {
	if n <= 0 {
		panic("interval must be > 0")
	}
	validateHour(hour)
	validateMinute(minute)

	// Anchor to start of today (midnight) to ensure consistent calculations
	now := time.Now()
	anchor := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	return Schedule{
		hour:       hour,
		minute:     minute,
		interval:   n,
		anchorDate: &anchor,
	}
}

func validateHour(hour int) {
	if hour < 0 || hour > 23 {
		panic("hour must be between 0 and 23")
	}
}

func validateMinute(minute int) {
	if minute < 0 || minute > 59 {
		panic("minute must be between 0 and 59")
	}
}

// In sets the timezone for the schedule and returns the modified schedule.
// By default, schedules use the local timezone. Use this to specify a different timezone.
//
// Example:
//
//	workers.DailyAt(9, 0).In(time.UTC)           // Daily at 9:00 AM UTC
//	workers.WeeklyAt(time.Monday, 9, 0).In(time.FixedZone("EST", -5*60*60))  // Monday at 9:00 AM EST
func (me Schedule) In(location *time.Location) Schedule {
	me.location = location
	return me
}

// String returns a human-readable description of the schedule.
func (me Schedule) String() string {
	timeStr := fmt.Sprintf("%02d:%02d", me.hour, me.minute)
	if me.weekday != nil {
		return fmt.Sprintf("%s at %s", me.weekday.String(), timeStr)
	}
	if me.interval > 0 {
		return fmt.Sprintf("every %d days at %s", me.interval, timeStr)
	}
	return fmt.Sprintf("daily at %s", timeStr)
}

// BackoffStrategy calculates the delay duration before the next retry attempt.
// The attempt parameter is 1-based (first retry is attempt 1).
type BackoffStrategy func(baseDelay time.Duration, attempt int) time.Duration

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
	//
	// Example: 5s, 5s, 5s, 5s...
	ConstantBackoff BackoffStrategy = func(baseDelay time.Duration, attempt int) time.Duration {
		return baseDelay
	}

	// LinearBackoff increases delay linearly with each attempt.
	// Formula: delay * attempt
	//
	// Example: 5s, 10s, 15s, 20s...
	LinearBackoff BackoffStrategy = func(baseDelay time.Duration, attempt int) time.Duration {
		return baseDelay * time.Duration(attempt)
	}

	// ExponentialBackoff doubles the delay with each attempt.
	// Formula: delay * 2^attempt
	//
	// Example: 5s, 10s, 20s, 40s...
	ExponentialBackoff BackoffStrategy = func(baseDelay time.Duration, attempt int) time.Duration {
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
		nRetries:        3,
		retryDelay:      5 * time.Second,
		backoffStrategy: ConstantBackoff,
	}

	for _, opt := range options {
		opt(&w)
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
		if tick <= 0 {
			panic("tick must be > 0")
		}
		w.tick = tick
	}
}

// WithTimeout sets the maximum duration allowed for each job execution.
// Must be > 0.
//
// Default: no timeout
func WithTimeout(timeout time.Duration) WorkerOption {
	return func(w *Worker) {
		if timeout <= 0 {
			panic("timeout must be > 0")
		}
		w.timeout = timeout
	}
}

// WithNRetries sets the number of retry attempts for failed jobs.
//
// Default: 3 retries
func WithNRetries(n int) WorkerOption {
	return func(w *Worker) {
		if n < 0 {
			panic("number of retries must be > 0")
		}
		w.nRetries = n
	}
}

// WithRetryDelay sets the delay between retry attempts.
// Must be > 0.
//
// Default: 5 seconds
func WithRetryDelay(delay time.Duration) WorkerOption {
	return func(w *Worker) {
		if delay <= 0 {
			panic("delay must be > 0")
		}
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

// WithSchedules sets one or more schedules for when the worker should run.
// When schedules are set, the worker ignores the tick interval and runs only at the specified times.
// Multiple schedules can be provided to run at different times.
//
// Example:
//
//	workers.WithSchedules(
//	    workers.DailyAt(9, 0),                    // Daily at 9:00 AM
//	    workers.WeeklyAt(time.Friday, 14, 30),    // Every Friday at 2:30 PM
//	    workers.EveryNDays(2, 2, 0),              // Every 2 days at 2:00 AM
//	)
func WithSchedules(schedules ...Schedule) WorkerOption {
	return func(w *Worker) {
		w.schedules = append(w.schedules, schedules...)
	}
}

// WithNRuns sets a limit on the total number of job executions.
// When the limit is reached, the worker stops automatically.
// Must be > 0.
//
// Default: unlimited runs
func WithNRuns(n int) WorkerOption {
	return func(w *Worker) {
		if n <= 0 {
			panic("number of runs must be > 0")
		}
		w.nRuns = n
	}
}

// start begins the worker's execution loop.
func (me *Worker) start(workerCtx context.Context) {
	me.logger.Info("worker started", "worker", me.name)
	defer me.logger.Info("worker stopped", "worker", me.name)

	firstRun := true

	for {
		waitDuration := me.getNextWaitDuration()
		if firstRun && len(me.schedules) == 0 {
			waitDuration = 0 // Run immediately for unscheduled workers
			firstRun = false
		}

		if waitDuration > 0 {
			me.logger.Info("next run",
				"worker", me.name,
				"in", waitDuration,
				"at", time.Now().Add(waitDuration).Format(time.RFC3339),
			)
		}

		select {
		case <-time.After(waitDuration):
			me.executeJob()
			if me.nRuns > 0 { // Avoid overflow by only incrementing for limited runs
				me.runCount++
				if me.runCount >= me.nRuns {
					me.logger.Info("worker reached run limit", "worker", me.name, "runs", me.runCount)
					return
				}
			}
		case <-workerCtx.Done():
			return
		}
	}
}

// getNextWaitDuration returns how long to wait until next execution.
// Handles both scheduled and tick-based workers.
func (me Worker) getNextWaitDuration() time.Duration {
	if len(me.schedules) > 0 {
		nextRun := me.getEarliestNextRun()
		return time.Until(nextRun)
	}
	return me.tick
}

// getEarliestNextRun finds the next scheduled run time across all schedules.
func (me Worker) getEarliestNextRun() time.Time {
	now := time.Now()
	earliest := me.calculateNextRun(me.schedules[0], now)

	for i := 1; i < len(me.schedules); i++ {
		candidate := me.calculateNextRun(me.schedules[i], now)
		if candidate.Before(earliest) {
			earliest = candidate
		}
	}

	return earliest
}

// calculateNextRun calculates the next run time for a single schedule.
func (me Worker) calculateNextRun(schedule Schedule, from time.Time) time.Time {
	loc := schedule.location
	if loc == nil {
		loc = time.Local
	}

	now := from.In(loc)
	next := time.Date(
		now.Year(), now.Month(), now.Day(),
		schedule.hour, schedule.minute, 0, 0, loc,
	)

	// If time already passed today, move to next occurrence
	if next.Before(now) {
		if schedule.weekday != nil {
			// Weekly schedule
			daysUntil := int((*schedule.weekday - now.Weekday() + 7) % 7)
			if daysUntil == 0 {
				daysUntil = 7 // Next week
			}
			next = next.AddDate(0, 0, daysUntil)
		} else if schedule.interval > 0 {
			// Every N days - calculate based on anchor date
			anchor := schedule.anchorDate.In(loc)
			daysSince := int(now.Sub(anchor).Hours() / 24)
			completedIntervals := daysSince / schedule.interval
			nextInterval := completedIntervals + 1
			nextDate := anchor.AddDate(0, 0, nextInterval*schedule.interval)
			next = time.Date(
				nextDate.Year(), nextDate.Month(), nextDate.Day(),
				schedule.hour, schedule.minute, 0, 0, loc,
			)
		} else {
			// Daily
			next = next.AddDate(0, 0, 1)
		}
	} else {
		// Time hasn't passed yet today
		if schedule.weekday != nil && now.Weekday() != *schedule.weekday {
			// Not the right day of week
			daysUntil := int(((*schedule.weekday) - now.Weekday() + 7) % 7)
			next = next.AddDate(0, 0, daysUntil)
		}
	}

	return next
}

// executeJob runs the job with retries.
func (me Worker) executeJob() {
	me.logger.Info("job started", "worker", me.name)

	// No timeout if value <= 0 (default).
	// Users can only set timeout to value > 0.
	jobCtx := context.Background()
	if me.timeout > 0 {
		var jobCtxCancel context.CancelFunc
		jobCtx, jobCtxCancel = context.WithTimeout(jobCtx, me.timeout)
		defer jobCtxCancel()
	}

	if err := me.job(jobCtx, me.logger); err != nil {
		me.logger.Error("job failed", "worker", me.name, "error", err)

		delay := me.retryDelay
		for i := 1; i <= me.nRetries; i++ {
			<-time.After(delay)
			me.logger.Info("job retry started", "worker", me.name, "retry", i)

			retryCtx, retryCtxCancel := context.WithTimeout(context.Background(), me.timeout)
			if err := me.job(retryCtx, me.logger); err != nil {
				me.logger.Error("job retry failed", "worker", me.name, "retry", i)
				retryCtxCancel()
				delay = me.backoffStrategy(me.retryDelay, i)
				continue
			}
			retryCtxCancel()

			me.logger.Info("job retry finished successfully", "worker", me.name, "retry", i)
			break
		}
	} else {
		me.logger.Info("job finished successfully", "worker", me.name)
	}
}
