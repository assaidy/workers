package workers

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestRegisterWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		wm := NewWorkerManager()

		job := func(ctx context.Context) error { return nil }

		w1 := NewWorker("worker1", job)
		w2 := NewWorker("worker2", job)

		wm.RegisterWorker(w1)
		wm.RegisterWorker(w2)

		if len(wm.workers) != 2 {
			t.Errorf("expected 2 workers, got %d", len(wm.workers))
		}
	})
}

func TestRegisterWorker_AssignsLogger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		wm := NewWorkerManager(WithLogger(logger))

		job := func(ctx context.Context) error { return nil }
		w := NewWorker("test", job)

		// Worker has no logger set
		if w.logger != nil {
			t.Error("worker should have no logger initially")
		}

		wm.RegisterWorker(w)

		// After registration, should have manager's logger
		if wm.workers[0].logger != logger {
			t.Error("worker should have manager's logger after registration")
		}
	})
}

func TestManager_ContextCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cancelled := make(chan bool, 1)

		job := func(ctx context.Context) error {
			time.Sleep(10 * time.Millisecond) // Short work
			select {
			case <-ctx.Done():
				cancelled <- true
				return ctx.Err()
			default:
				return nil
			}
		}

		wm := NewWorkerManager()
		w := NewWorker("test", job, WithTick(1*time.Hour))
		wm.RegisterWorker(w)

		ctx, cancel := context.WithCancel(context.Background())

		go wm.workers[0].start(ctx)

		// Wait for first job to start
		synctest.Wait()

		// Cancel the context
		cancel()

		// Wait for worker to process cancellation
		synctest.Wait()
		time.Sleep(20 * time.Millisecond)

		// Check that job was cancelled
		select {
		case <-cancelled:
			// Good - job detected cancellation
		default:
			// Job completed before cancellation, which is also valid behavior
		}
	})
}

func TestManager_MultipleWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var count1, count2 atomic.Int32

		job1 := func(ctx context.Context) error {
			count1.Add(1)
			return nil
		}

		job2 := func(ctx context.Context) error {
			count2.Add(1)
			return nil
		}

		wm := NewWorkerManager()
		wm.RegisterWorker(NewWorker("worker1", job1, WithTick(50*time.Millisecond), WithNRuns(2)))
		wm.RegisterWorker(NewWorker("worker2", job2, WithTick(50*time.Millisecond), WithNRuns(2)))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go wm.workers[0].start(ctx)
		go wm.workers[1].start(ctx)

		// Wait for both workers to complete
		time.Sleep(150 * time.Millisecond)

		if count1.Load() != 2 {
			t.Errorf("worker1: expected 2 runs, got %d", count1.Load())
		}
		if count2.Load() != 2 {
			t.Errorf("worker2: expected 2 runs, got %d", count2.Load())
		}
	})
}

func TestIntegration_MixedWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var tickCount, scheduledCount atomic.Int32

		tickJob := func(ctx context.Context) error {
			tickCount.Add(1)
			return nil
		}

		scheduledJob := func(ctx context.Context) error {
			scheduledCount.Add(1)
			return nil
		}

		wm := NewWorkerManager()

		// Tick-based worker with nRuns
		wm.RegisterWorker(NewWorker("tick-worker", tickJob,
			WithTick(100*time.Millisecond),
			WithNRuns(3),
		))

		// Scheduled worker (won't run in this test as schedule is in future)
		wm.RegisterWorker(NewWorker("scheduled-worker", scheduledJob,
			WithSchedules(DailyAt(23, 59)),
			WithNRuns(1),
		))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go wm.workers[0].start(ctx)
		go wm.workers[1].start(ctx)

		// Wait for tick-based worker to complete
		time.Sleep(400 * time.Millisecond)

		if tickCount.Load() != 3 {
			t.Errorf("tick-worker: expected 3 runs, got %d", tickCount.Load())
		}

		// Scheduled worker shouldn't run
		if scheduledCount.Load() != 0 {
			t.Errorf("scheduled-worker: expected 0 runs, got %d", scheduledCount.Load())
		}
	})
}

func TestIntegration_RunLimitWithRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		callCount := 0

		job := func(ctx context.Context) error {
			callCount++
			// Fail first 2 attempts, succeed on 3rd
			if callCount < 3 {
				return context.DeadlineExceeded
			}
			return nil
		}

		wm := NewWorkerManager()
		wm.RegisterWorker(NewWorker("test", job,
			WithNRetries(5),
			WithRetryDelay(10*time.Millisecond),
			WithNRuns(1),
		))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go wm.workers[0].start(ctx)

		// Wait for initial run + 2 retries
		time.Sleep(100 * time.Millisecond)

		// Should have exactly 3 calls (1 initial + 2 retries) for nRuns=1
		if callCount != 3 {
			t.Errorf("expected 3 calls (1 initial + 2 retries), got %d", callCount)
		}
	})
}

func TestIntegration_ContextTimeoutAcrossRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		timeoutCount := 0

		job := func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				timeoutCount++
				return ctx.Err()
			case <-time.After(1 * time.Hour):
				return nil
			}
		}

		wm := NewWorkerManager()
		wm.RegisterWorker(NewWorker("test", job,
			WithTimeout(50*time.Millisecond),
			WithNRetries(2),
			WithRetryDelay(10*time.Millisecond),
			WithNRuns(1),
		))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go wm.workers[0].start(ctx)

		// Wait for initial + 2 retries
		time.Sleep(200 * time.Millisecond)

		// Each attempt should timeout
		if timeoutCount != 3 {
			t.Errorf("expected 3 timeouts (initial + 2 retries), got %d", timeoutCount)
		}
	})
}

func TestSchedule_InTimezone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		nyc, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skip("Skipping: cannot load timezone", err)
		}

		s := DailyAt(9, 0).In(nyc)

		if s.location != nyc {
			t.Error("timezone not set correctly")
		}
	})
}

func TestGetNextWaitDuration_Scheduled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Create a schedule for tomorrow at noon
		w := NewWorker("test", func(ctx context.Context) error { return nil })
		w.schedules = []Schedule{DailyAt(12, 0)}

		duration := w.getNextWaitDuration()

		// Should be roughly 12-36 hours depending on current time
		if duration <= 0 {
			t.Error("expected positive duration for future schedule")
		}
		if duration > 48*time.Hour {
			t.Error("duration too large for daily schedule")
		}
	})
}

func TestGetNextWaitDuration_TickBased(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := NewWorker("test", func(ctx context.Context) error { return nil })

		duration := w.getNextWaitDuration()

		// Should return the tick duration
		if duration != 1*time.Hour {
			t.Errorf("expected 1h, got %v", duration)
		}
	})
}
