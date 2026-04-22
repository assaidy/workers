package workers

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
)

// WorkerManager coordinates multiple workers, managing their lifecycle from startup
// to graceful shutdown. It handles OS signals (SIGINT, SIGTERM) and recovers from
// worker panics to ensure system stability.
type WorkerManager struct {
	workers []Worker
	logger  *slog.Logger

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// NewWorkerManager creates a manager for coordinating workers.
// Configure with options before registering workers and calling Start.
func NewWorkerManager(options ...WorkerManagerOption) *WorkerManager {
	wm := &WorkerManager{}
	for _, opt := range options {
		opt(wm)
	}
	if wm.logger == nil {
		wm.logger = slog.Default()
	}
	return wm
}

// WorkerManagerOption configures a WorkerManager. Use WithLogger to create options.
type WorkerManagerOption func(*WorkerManager)

// WithLogger sets the default logger for the manager and all workers without their own logger.
//
// Default: slog.Default()
func WithLogger(logger *slog.Logger) WorkerManagerOption {
	return func(wm *WorkerManager) {
		wm.logger = logger
	}
}

// RegisterWorker adds a worker to the manager. The worker inherits the manager's
// logger if it doesn't have its own. Workers must be registered before Start is called.
func (me *WorkerManager) RegisterWorker(worker Worker) {
	worker.logger = me.logger
	me.workers = append(me.workers, worker)
}

func (me *WorkerManager) Start() {
	me.ctx, me.cancel = context.WithCancel(context.Background())

	me.logger.Info("starting workers", "count", len(me.workers))
	defer me.logger.Info("started all workers")

	for _, w := range me.workers {
		me.wg.Go(func() {
			defer func() {
				if err := recover(); err != nil {
					me.logger.Error("worker panic", "worker", w.name, "error", err, "trace", debug.Stack())
				}
			}()

			w.start(me.ctx)
		})
	}
}

func (me *WorkerManager) Stop() {
	me.logger.Info("stopping workers", "count", len(me.workers))
	defer me.logger.Info("stopped all workers")
	me.cancel()
	me.wg.Wait()
}
