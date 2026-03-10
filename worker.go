package workers

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"
)

// Worker manages a background job with configurable execution intervals,
// retry logic, timeouts, and scheduling. Workers can run periodically or
// at specific times defined by schedules.
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

// WorkerJob defines the function signature for job implementations.
// The function receives a context for cancellation and timeout handling.
type WorkerJob func(context.Context) error

// Schedule defines when a worker should execute. Use DailyAt, WeeklyAt,
// or EveryNDays to create schedules. Modify with the In method to set timezones.
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
// Examples: "daily at 09:00", "Monday at 14:30", "every 2 days at 02:00"
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

// BackoffStrategy defines how long to wait between retry attempts.
// Implement this interface to create custom backoff strategies.
//
// The worker calls GetNextDelay() to get the next retry delay.
// SetBaseDelay(baseDelay) is called when the worker is created to set the initial delay.
// Reset() is called after a successful retry to reset the backoff state for the next failure cycle.
//
// Example:
//
//	type myBackoff struct {
//	    workers.BaseBackoff
//	}
//
//	func (m *myBackoff) SetBaseDelay(baseDelay time.Duration) {
//	    m.BaseDelay = baseDelay
//	}
//
//	func (m *myBackoff) GetNextDelay() time.Duration {
//	    m.Attempt += 1
//	    return m.BaseDelay * time.Duration(m.Attempt)
//	}
//
//	func (m *myBackoff) Reset() {
//	    m.Attempt = 0
//	}
type BackoffStrategy interface {
	SetBaseDelay(baseDelay time.Duration)
	GetNextDelay() time.Duration
	Reset()
}

// BaseBackoff provides common state for backoff strategies.
// Embed this struct in your custom backoff implementation.
type BaseBackoff struct {
	// Attempt is the current retry attempt number.
	// It is incremented each time GetNextDelay() is called.
	Attempt int

	// BaseDelay is the initial delay set by SetBaseDelay().
	// This is typically the retryDelay configured on the worker.
	BaseDelay time.Duration

	// CapDelay is the maximum delay cap.
	// Used by jitter strategies to limit maximum backoff time.
	CapDelay time.Duration

	// PreviousDelay is the delay returned by the last GetNextDelay() call.
	// Used by decorrelated jitter to calculate the next delay.
	PreviousDelay time.Duration
}

func (me *BaseBackoff) SetBaseDelay(baseDelay time.Duration) {
	me.BaseDelay = baseDelay
	me.PreviousDelay = baseDelay
}

func (me *BaseBackoff) Reset() {
	me.Attempt = 0
	me.PreviousDelay = me.BaseDelay
}

type constantBackoff struct {
	BaseBackoff
}

func (me *constantBackoff) GetNextDelay() time.Duration {
	me.Attempt += 1
	return me.BaseDelay
}

// ConstantBackoff returns a fixed delay regardless of attempt number.
//
// Formula:
//
//	delay = baseDelay
//
// Example (base=5s):
//
//	5s, 5s, 5s, 5s...
func ConstantBackoff() BackoffStrategy {
	return &constantBackoff{}
}

type linearBackoff struct {
	BaseBackoff
}

func (me *linearBackoff) GetNextDelay() time.Duration {
	me.Attempt += 1
	return me.BaseDelay * time.Duration(me.Attempt)
}

// LinearBackoff increases delay linearly with each attempt.
//
// Formula:
//
//	delay = baseDelay * attempt
//
// Example (base=5s):
//
//	5s, 10s, 15s, 20s...
func LinearBackoff() BackoffStrategy {
	return &linearBackoff{}
}

type exponentialBackoff struct {
	BaseBackoff
}

func (me *exponentialBackoff) GetNextDelay() time.Duration {
	me.Attempt += 1
	return me.BaseDelay << me.Attempt
}

// ExponentialBackoff doubles the delay with each attempt.
//
// Formula:
//
//	delay = baseDelay * 2^attempt
//
// Example (base=5s):
//
//	5s, 10s, 20s, 40s...
func ExponentialBackoff() BackoffStrategy {
	return &exponentialBackoff{}
}

type fullJitterBackoff struct {
	BaseBackoff
}

func (me *fullJitterBackoff) GetNextDelay() time.Duration {
	me.Attempt += 1
	maxDelay := min(me.CapDelay, me.BaseDelay<<me.Attempt)
	return time.Duration(rand.Int64N(int64(maxDelay) + 1))
}

// FullJitterBackoff returns an exponential backoff strategy with full jitter.
// The exponential delay is used as a cap, and the actual delay is randomly
// chosen between 0 and that cap.
//
// Formula:
//
//	cap = min(capDelay, baseDelay * 2^attempt)
//	delay = random(0, cap)
//
// This strategy spreads retries uniformly to reduce synchronized retry spikes.
//
// Example (base=5s, cap=40s):
//
//	random(0–10s), random(0–20s), random(0–40s), random(0–40s)...
func FullJitterBackoff(capDelay time.Duration) BackoffStrategy {
	return &fullJitterBackoff{BaseBackoff: BaseBackoff{CapDelay: capDelay}}
}

type equalJitterBackoff struct {
	BaseBackoff
}

func (me *equalJitterBackoff) GetNextDelay() time.Duration {
	me.Attempt += 1
	maxDelay := min(me.CapDelay, me.BaseDelay<<me.Attempt)
	half := maxDelay >> 1
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// EqualJitterBackoff returns an exponential backoff strategy with equal jitter.
// Half of the delay is deterministic and the other half is randomized.
//
// Formula:
//
//	cap = min(capDelay, baseDelay * 2^attempt)
//	delay = cap/2 + random(0, cap/2)
//
// Compared to Full Jitter, this guarantees a minimum delay while still
// spreading retries to reduce contention.
//
// Example (base=5s, cap=40s):
//
//	5s + random(0–5s)
//	10s + random(0–10s)
//	20s + random(0–20s)
//	20s + random(0–20s)...
func EqualJitterBackoff(capDelay time.Duration) BackoffStrategy {
	return &equalJitterBackoff{BaseBackoff: BaseBackoff{CapDelay: capDelay}}
}

type decorrelatedJitterBackoff struct {
	BaseBackoff
}

func (me *decorrelatedJitterBackoff) GetNextDelay() time.Duration {
	me.Attempt += 1
	maxDelay := me.PreviousDelay * 3
	random := me.BaseDelay + time.Duration(rand.Int64N(int64(maxDelay-me.BaseDelay)+1))
	me.PreviousDelay = min(me.CapDelay, random)
	return me.PreviousDelay
}

// DecorrelatedJitterBackoff returns an exponential backoff strategy where
// the next delay depends on the previous delay, with added randomness.
//
// Instead of strictly doubling the delay, this method randomly selects
// a value between baseDelay and three times the previous delay, then
// clamps it to capDelay.
//
// Formula:
//
//	delay = min(capDelay, random(baseDelay, previousDelay * 3))
//
// This produces a wider distribution of retry delays and helps prevent
// retry synchronization while still allowing delays to grow over time.
//
// Example (base=5s, cap=40s):
//
//	random(5–15s)
//	random(5–45s)
//	random(5–120s) → capped at 40s
//	random(5–120s) → capped at 40s...
func DecorrelatedJitterBackoff(capDelay time.Duration) BackoffStrategy {
	return &decorrelatedJitterBackoff{BaseBackoff: BaseBackoff{CapDelay: capDelay}}
}

// NewWorker creates a worker with the specified name and job function.
// Apply options to configure tick intervals, timeouts, retries, schedules,
// and other behaviors. Panics if any option is invalid.
func NewWorker(name string, job WorkerJob, options ...WorkerOption) Worker {
	w := Worker{
		name:            name,
		job:             job,
		tick:            1 * time.Hour,
		nRetries:        3,
		retryDelay:      5 * time.Second,
		backoffStrategy: ConstantBackoff(),
	}

	for _, opt := range options {
		opt(&w)
	}

	w.backoffStrategy.SetBaseDelay(w.retryDelay)

	return w
}

// WorkerOption configures a Worker. Use the With* functions to create options.
type WorkerOption func(*Worker)

// WithItsOwnLogger sets a worker-specific logger, overriding the WorkerManager's logger.
// The logger must not be nil.
//
// Default: inherits from WorkerManager
func WithItsOwnLogger(logger *slog.Logger) WorkerOption {
	return func(w *Worker) {
		if logger == nil {
			panic("cannot explicitly set a nil logger")
		}
		w.logger = logger
	}
}

// WithTick sets the interval between job executions.
// Ignored when schedules are set.
// The tick must be greater than 0.
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

// WithTimeout sets a maximum execution time for each job run.
// Jobs exceeding this duration are cancelled via context.
// Must be greater than 0.
//
// Default: no timeout (jobs run indefinitely)
func WithTimeout(timeout time.Duration) WorkerOption {
	return func(w *Worker) {
		if timeout <= 0 {
			panic("timeout must be > 0")
		}
		w.timeout = timeout
	}
}

// WithNRetries sets how many times to retry a failed job.
// Set to 0 to disable retries.
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

// WithRetryDelay sets the initial delay between retry attempts.
// The actual delay may increase based on the backoff strategy. Must be greater than 0.
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

// WithBackoffStrategy sets how the retry delay increases with each attempt.
// Use ConstantBackoff, LinearBackoff, ExponentialBackoff, or a custom function.
//
// Default: ConstantBackoff
func WithBackoffStrategy(strategy BackoffStrategy) WorkerOption {
	return func(w *Worker) {
		w.backoffStrategy = strategy
	}
}

// WithSchedules sets specific times for the worker to run, replacing tick-based execution.
// When schedules are configured, the worker runs only at the specified times.
// Multiple schedules can be combined to run at different times.
//
// Example:
//
//	workers.WithSchedules(
//	    workers.DailyAt(9, 0),
//	    workers.WeeklyAt(time.Friday, 14, 30),
//	    workers.EveryNDays(2, 2, 0),
//	)
func WithSchedules(schedules ...Schedule) WorkerOption {
	return func(w *Worker) {
		w.schedules = append(w.schedules, schedules...)
	}
}

// WithNRuns limits the total number of times a worker executes.
// After reaching the limit, the worker stops automatically. Must be greater than 0.
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

	jobCtx, jobCtxCancel := me.getRunCtx()
	defer jobCtxCancel()

	if err := me.job(jobCtx); err != nil {
		me.logger.Error("job failed", "worker", me.name, "error", err)

		delay := me.retryDelay
		for i := 1; i <= me.nRetries; i++ {
			<-time.After(delay)
			me.logger.Info("job retry started", "worker", me.name, "retry", i)

			retryCtx, retryCtxCancel := me.getRunCtx()
			if err := me.job(retryCtx); err != nil {
				me.logger.Error("job retry failed", "worker", me.name, "retry", i)
				retryCtxCancel()
				delay = me.backoffStrategy.GetNextDelay()
				continue
			}
			retryCtxCancel()
			me.backoffStrategy.Reset()

			me.logger.Info("job retry finished successfully", "worker", me.name, "retry", i)
			break
		}
	} else {
		me.logger.Info("job finished successfully", "worker", me.name)
	}
}

func (me Worker) getRunCtx() (context.Context, context.CancelFunc) {
	if me.timeout > 0 {
		return context.WithTimeout(context.Background(), me.timeout)
	}
	// No timeout if value <= 0 (default).
	// Users can only set timeout to value > 0.
	return context.WithCancel(context.Background())
}
