package workers

import (
	"context"
	"log/slog"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
)

// WorkerManager manages a collection of workers, handling their lifecycle
// including startup, graceful shutdown on signals, and panic recovery.
type WorkerManager struct {
	workers []Worker
	logger  *slog.Logger
}

// NewWorkerManager creates a new WorkerManager with the given options.
func NewWorkerManager(options ...WorkerManagerOption) WorkerManager {
	wm := WorkerManager{}
	for _, opt := range options {
		opt(&wm)
	}
	if wm.logger == nil {
		wm.logger = slog.Default()
	}
	return wm
}

// WorkerManagerOption is a function that configures a WorkerManager.
type WorkerManagerOption func(*WorkerManager)

// WithLogger sets the logger for the WorkerManager and all workers that don't have their own.
//
// Default: slog.Default()
func WithLogger(logger *slog.Logger) WorkerManagerOption {
	return func(wm *WorkerManager) {
		wm.logger = logger
	}
}

// RegisterWorker registers a worker to the manager.
func (me *WorkerManager) RegisterWorker(worker Worker) {
	if worker.logger == nil {
		worker.logger = me.logger
	}
	me.workers = append(me.workers, worker)
}

// Start begins execution of all registered workers.
// It blocks until SIGINT or SIGTERM is received, then gracefully shuts down all workers.
func (me WorkerManager) Start() {
	quitCtx, quitCtxCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	workersWg := sync.WaitGroup{}
	defer workersWg.Wait()

	me.logger.Info("starting workers", "count", len(me.workers))
	for _, w := range me.workers {
		workersWg.Go(func() {
			defer func() {
				if err := recover(); err != nil {
					me.logger.Error("worker panic", "worker", w.name, "error", err, "trace", debug.Stack())
				}
			}()

			w.start(quitCtx)
		})
	}

	<-quitCtx.Done()
	quitCtxCancel()
	me.logger.Info("gracefully stopping workers. press Ctrl-c to force stop.")
}
