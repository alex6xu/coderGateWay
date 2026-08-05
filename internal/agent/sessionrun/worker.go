package sessionrun

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Executor func(ctx context.Context, run *Run) error

type WorkerConfig struct {
	Store        *Store
	Execute      Executor
	PollInterval time.Duration
}

type Worker struct{ config WorkerConfig }

func NewWorker(cfg WorkerConfig) *Worker {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	return &Worker{config: cfg}
}

func (w *Worker) RecoverInterrupted() (int64, error) {
	if w.config.Store == nil {
		return 0, errors.New("session run worker has no store")
	}
	return w.config.Store.RecoverInterrupted()
}

func (w *Worker) Run(ctx context.Context) error {
	if w.config.Store == nil || w.config.Execute == nil {
		return errors.New("session run worker is not configured")
	}
	for {
		processed, err := w.ProcessNext(ctx)
		if err != nil {
			return err
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(w.config.PollInterval):
		}
	}
}

func (w *Worker) ProcessNext(ctx context.Context) (bool, error) {
	run, err := w.config.Store.ClaimNext()
	if errors.Is(err, ErrNoQueuedRun) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim session run: %w", err)
	}
	execErr := w.config.Execute(ctx, run)
	if execErr != nil {
		if cur, _ := w.config.Store.GetByID(run.ID); cur != nil && cur.Status == StatusRunning {
			_ = w.config.Store.Finish(run.ID, StatusFailed, execErr.Error())
		}
		return true, nil
	}
	return true, nil
}
