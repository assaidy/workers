package workers

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"testing/synctest"
	"time"
)

func TestNewWorker_WithDefaults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		callCount := 0
		job := func(ctx context.Context, log *slog.Logger) error {
			callCount++
			return nil
		}

		w := NewWorker("test", job)

		if w.name != "test" {
			t.Errorf("expected name 'test', got '%s'", w.name)
		}
		if w.tick != 1*time.Hour {
			t.Errorf("expected tick 1h, got %v", w.tick)
		}
		if w.nRetries != 3 {
			t.Errorf("expected 3 retries, got %d", w.nRetries)
		}
		if w.retryDelay != 5*time.Second {
			t.Errorf("expected 5s retry delay, got %v", w.retryDelay)
		}
		if w.backoffStrategy == nil {
			t.Error("expected backoff strategy to be set")
		}
		if w.nRuns != 0 {
			t.Errorf("expected 0 nRuns (unlimited), got %d", w.nRuns)
		}
		if w.runCount != 0 {
			t.Errorf("expected 0 runCount, got %d", w.runCount)
		}
	})
}

func TestNewWorker_WithAllOptions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		job := func(ctx context.Context, log *slog.Logger) error { return nil }

		w := NewWorker("test", job,
			WithItsOwnLogger(logger),
			WithTick(30*time.Second),
			WithTimeout(5*time.Minute),
			WithNRetries(5),
			WithRetryDelay(10*time.Second),
			WithBackoffStrategy(LinearBackoff),
			WithSchedules(DailyAt(9, 0)),
			WithNRuns(10),
		)

		if w.tick != 30*time.Second {
			t.Errorf("expected tick 30s, got %v", w.tick)
		}
		if w.timeout != 5*time.Minute {
			t.Errorf("expected timeout 5m, got %v", w.timeout)
		}
		if w.nRetries != 5 {
			t.Errorf("expected 5 retries, got %d", w.nRetries)
		}
		if w.retryDelay != 10*time.Second {
			t.Errorf("expected 10s retry delay, got %v", w.retryDelay)
		}
		if w.nRuns != 10 {
			t.Errorf("expected 10 nRuns, got %d", w.nRuns)
		}
		if len(w.schedules) != 1 {
			t.Errorf("expected 1 schedule, got %d", len(w.schedules))
		}
	})
}

func TestNewWorker_PanicsOnInvalidOptions(t *testing.T) {
	job := func(ctx context.Context, log *slog.Logger) error { return nil }

	tests := []struct {
		name    string
		opt     WorkerOption
		wantErr string
	}{
		{
			name:    "nil logger",
			opt:     WithItsOwnLogger(nil),
			wantErr: "cannot explicitly set a nil logger",
		},
		{
			name:    "zero tick",
			opt:     WithTick(0),
			wantErr: "tick must be > 0",
		},
		{
			name:    "negative tick",
			opt:     WithTick(-1 * time.Second),
			wantErr: "tick must be > 0",
		},
		{
			name:    "zero timeout",
			opt:     WithTimeout(0),
			wantErr: "timeout must be > 0",
		},
		{
			name:    "negative timeout",
			opt:     WithTimeout(-1 * time.Second),
			wantErr: "timeout must be > 0",
		},
		{
			name:    "negative nRetries",
			opt:     WithNRetries(-1),
			wantErr: "number of retries must be > 0",
		},
		{
			name:    "zero retryDelay",
			opt:     WithRetryDelay(0),
			wantErr: "delay must be > 0",
		},
		{
			name:    "negative retryDelay",
			opt:     WithRetryDelay(-1 * time.Second),
			wantErr: "delay must be > 0",
		},
		{
			name:    "zero nRuns",
			opt:     WithNRuns(0),
			wantErr: "number of runs must be > 0",
		},
		{
			name:    "negative nRuns",
			opt:     WithNRuns(-1),
			wantErr: "number of runs must be > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if r != tt.wantErr {
						t.Errorf("expected panic '%v', got '%v'", tt.wantErr, r)
					}
				} else {
					t.Errorf("expected panic but didn't get one")
				}
			}()
			NewWorker("test", job, tt.opt)
		})
	}
}

func TestWorker_RespectsNRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		callCount := 0
		job := func(ctx context.Context, log *slog.Logger) error {
			callCount++
			return nil
		}

		w := NewWorker("test", job,
			WithTick(100*time.Millisecond),
			WithNRuns(3),
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		// Wait for all 3 runs (first immediate + 2 ticks)
		// First run happens immediately
		synctest.Wait()

		// Advance time for next 2 runs
		time.Sleep(250 * time.Millisecond)

		if callCount != 3 {
			t.Errorf("expected 3 runs, got %d", callCount)
		}
	})
}

func TestWorker_RunOnceThenStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		callCount := 0
		job := func(ctx context.Context, log *slog.Logger) error {
			callCount++
			return nil
		}

		w := NewWorker("test", job,
			WithNRuns(1),
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		synctest.Wait()
		time.Sleep(10 * time.Millisecond)

		if callCount != 1 {
			t.Errorf("expected 1 run, got %d", callCount)
		}
	})
}

func TestWorker_ZeroRuns_NotCounting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		callCount := 0
		job := func(ctx context.Context, log *slog.Logger) error {
			callCount++
			return nil
		}

		w := NewWorker("test", job,
			WithTick(50*time.Millisecond),
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		// Wait for 2 "ticks" worth of time
		time.Sleep(120 * time.Millisecond)

		cancel()
		synctest.Wait()

		// With unlimited runs, runCount should remain 0
		if w.runCount != 0 {
			t.Errorf("expected runCount to stay 0 for unlimited runs, got %d", w.runCount)
		}
		if callCount < 2 {
			t.Errorf("expected at least 2 calls for unlimited runs, got %d", callCount)
		}
	})
}

func TestWorker_ExecutesImmediatelyOnStartup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		callCount := 0
		done := make(chan bool, 1)

		job := func(ctx context.Context, log *slog.Logger) error {
			callCount++
			done <- true
			return nil
		}

		w := NewWorker("test", job,
			WithTick(1*time.Hour), // Long tick, shouldn't matter for first run
			WithNRuns(1),
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		// Should complete immediately without waiting for tick
		select {
		case <-done:
			// Success - ran immediately
		case <-time.After(100 * time.Millisecond):
			t.Error("worker did not execute immediately on startup")
		}

		if callCount != 1 {
			t.Errorf("expected 1 run, got %d", callCount)
		}
	})
}

func TestWorker_ScheduledWorkersWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		callCount := 0
		job := func(ctx context.Context, log *slog.Logger) error {
			callCount++
			return nil
		}

		// Schedule for a time in the future (tomorrow at noon)
		w := NewWorker("test", job,
			WithSchedules(DailyAt(12, 0)),
			WithNRuns(1),
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		// Should NOT run immediately - should wait until scheduled time
		time.Sleep(50 * time.Millisecond)

		if callCount != 0 {
			t.Error("scheduled worker ran before scheduled time")
		}

		cancel()
	})
}

func TestWorker_RespectsTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		timeoutHit := false
		job := func(ctx context.Context, log *slog.Logger) error {
			select {
			case <-ctx.Done():
				timeoutHit = true
				return ctx.Err()
			case <-time.After(1 * time.Hour):
				return nil
			}
		}

		w := NewWorker("test", job,
			WithTimeout(100*time.Millisecond),
			WithNRetries(0), // No retries to avoid getting stuck
			WithNRuns(1),
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		// Wait for job to start and timeout
		time.Sleep(150 * time.Millisecond)

		if !timeoutHit {
			t.Error("job was not cancelled by timeout")
		}
	})
}

func TestWorker_NoTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		jobCompleted := make(chan bool, 1)
		job := func(ctx context.Context, log *slog.Logger) error {
			time.Sleep(50 * time.Millisecond)
			jobCompleted <- true
			return nil
		}

		w := NewWorker("test", job,
			WithNRuns(1),
			// No WithTimeout - should default to no timeout
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		// Wait for job to complete
		select {
		case <-jobCompleted:
			// Good
		case <-time.After(100 * time.Millisecond):
			t.Error("job did not complete (should have no timeout)")
		}
	})
}

func TestWorker_RetriesOnFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		callCount := 0
		job := func(ctx context.Context, log *slog.Logger) error {
			callCount++
			if callCount < 3 {
				return errors.New("temporary error")
			}
			return nil
		}

		w := NewWorker("test", job,
			WithNRetries(3),
			WithRetryDelay(10*time.Millisecond),
			WithNRuns(1),
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		// Wait for initial run + retries
		time.Sleep(100 * time.Millisecond)

		if callCount != 3 {
			t.Errorf("expected 3 calls (1 initial + 2 retries), got %d", callCount)
		}
	})
}

func TestWorker_NoRetriesOnSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		callCount := 0
		job := func(ctx context.Context, log *slog.Logger) error {
			callCount++
			return nil
		}

		w := NewWorker("test", job,
			WithNRetries(3),
			WithNRuns(1),
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		synctest.Wait()

		if callCount != 1 {
			t.Errorf("expected 1 call (no retries on success), got %d", callCount)
		}
	})
}

func TestWorker_RetryWithTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		retryTimeoutCount := 0
		job := func(ctx context.Context, log *slog.Logger) error {
			select {
			case <-ctx.Done():
				retryTimeoutCount++
				return ctx.Err()
			case <-time.After(1 * time.Hour):
				return nil
			}
		}

		w := NewWorker("test", job,
			WithTimeout(50*time.Millisecond),
			WithNRetries(2),
			WithRetryDelay(10*time.Millisecond),
			WithNRuns(1),
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		// Wait for initial run + 2 retries to timeout
		time.Sleep(200 * time.Millisecond)

		// All 3 attempts (initial + 2 retries) should timeout
		if retryTimeoutCount != 3 {
			t.Errorf("expected 3 timeouts (initial + 2 retries), got %d", retryTimeoutCount)
		}
	})
}

func TestWorker_RetryWithoutTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		callCount := 0
		job := func(ctx context.Context, log *slog.Logger) error {
			// Check that context has no deadline (no timeout set)
			_, hasDeadline := ctx.Deadline()
			if hasDeadline {
				t.Error("retry context should not have a deadline when no timeout is configured")
			}
			callCount++
			if callCount < 3 {
				return errors.New("temporary error")
			}
			return nil
		}

		w := NewWorker("test", job,
			WithNRetries(3),
			WithRetryDelay(10*time.Millisecond),
			WithNRuns(1),
			// No WithTimeout - should default to no timeout
		)
		w.logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.start(ctx)

		// Wait for initial run + 2 retries
		time.Sleep(100 * time.Millisecond)

		if callCount != 3 {
			t.Errorf("expected 3 calls (1 initial + 2 retries), got %d", callCount)
		}
	})
}

func TestConstantBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		baseDelay := 100 * time.Millisecond

		// Should always return the same delay
		d1 := ConstantBackoff(baseDelay, 1)
		d2 := ConstantBackoff(baseDelay, 5)
		d3 := ConstantBackoff(baseDelay, 10)

		if d1 != baseDelay || d2 != baseDelay || d3 != baseDelay {
			t.Error("ConstantBackoff should return fixed delay")
		}
	})
}

func TestLinearBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		baseDelay := 100 * time.Millisecond

		d1 := LinearBackoff(baseDelay, 1)
		d2 := LinearBackoff(baseDelay, 2)
		d3 := LinearBackoff(baseDelay, 3)

		if d1 != 100*time.Millisecond {
			t.Errorf("attempt 1: expected 100ms, got %v", d1)
		}
		if d2 != 200*time.Millisecond {
			t.Errorf("attempt 2: expected 200ms, got %v", d2)
		}
		if d3 != 300*time.Millisecond {
			t.Errorf("attempt 3: expected 300ms, got %v", d3)
		}
	})
}

func TestExponentialBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		baseDelay := 100 * time.Millisecond

		d1 := ExponentialBackoff(baseDelay, 1)
		d2 := ExponentialBackoff(baseDelay, 2)
		d3 := ExponentialBackoff(baseDelay, 3)

		if d1 != 200*time.Millisecond {
			t.Errorf("attempt 1: expected 200ms, got %v", d1)
		}
		if d2 != 400*time.Millisecond {
			t.Errorf("attempt 2: expected 400ms, got %v", d2)
		}
		if d3 != 800*time.Millisecond {
			t.Errorf("attempt 3: expected 800ms, got %v", d3)
		}
	})
}

func TestDailyAt_Valid(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := DailyAt(9, 30)

		if s.hour != 9 {
			t.Errorf("expected hour 9, got %d", s.hour)
		}
		if s.minute != 30 {
			t.Errorf("expected minute 30, got %d", s.minute)
		}
		if s.weekday != nil {
			t.Error("expected no weekday for DailyAt")
		}
		if s.interval != 0 {
			t.Error("expected no interval for DailyAt")
		}
	})
}

func TestDailyAt_InvalidHour(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for invalid hour")
			}
		}()
		DailyAt(25, 0)
	})
}

func TestDailyAt_InvalidMinute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for invalid minute")
			}
		}()
		DailyAt(12, 60)
	})
}

func TestWeeklyAt_Valid(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := WeeklyAt(time.Monday, 14, 30)

		if s.hour != 14 {
			t.Errorf("expected hour 14, got %d", s.hour)
		}
		if s.minute != 30 {
			t.Errorf("expected minute 30, got %d", s.minute)
		}
		if s.weekday == nil {
			t.Fatal("expected weekday to be set")
		}
		if *s.weekday != time.Monday {
			t.Errorf("expected Monday, got %v", *s.weekday)
		}
	})
}

func TestEveryNDays_Valid(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := EveryNDays(3, 10, 0)

		if s.hour != 10 {
			t.Errorf("expected hour 10, got %d", s.hour)
		}
		if s.minute != 0 {
			t.Errorf("expected minute 0, got %d", s.minute)
		}
		if s.interval != 3 {
			t.Errorf("expected interval 3, got %d", s.interval)
		}
		if s.anchorDate == nil {
			t.Error("expected anchorDate to be set")
		}
	})
}

func TestEveryNDays_InvalidInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for invalid interval")
			}
		}()
		EveryNDays(0, 12, 0)
	})
}

func TestSchedule_String(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tests := []struct {
			schedule Schedule
			want     string
		}{
			{DailyAt(9, 0), "daily at 09:00"},
			{WeeklyAt(time.Monday, 14, 30), "Monday at 14:30"},
			{EveryNDays(2, 10, 0), "every 2 days at 10:00"},
		}

		for _, tt := range tests {
			got := tt.schedule.String()
			if got != tt.want {
				t.Errorf("Schedule.String() = %q, want %q", got, tt.want)
			}
		}
	})
}
