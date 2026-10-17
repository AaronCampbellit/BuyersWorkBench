package jobs

import (
	"buyersworkbench/internal/workbench"
	"context"
	"errors"
	"github.com/riverqueue/river"
	"time"
)

type CollectionWorker struct {
	river.WorkerDefaults[workbench.CollectionArgs]
	Service *workbench.Service
}

func (*CollectionWorker) Timeout(*river.Job[workbench.CollectionArgs]) time.Duration {
	return 45 * time.Second
}
func (w *CollectionWorker) Work(ctx context.Context, job *river.Job[workbench.CollectionArgs]) error {
	err := w.Service.Collect(ctx, job.Args)
	if errors.Is(err, workbench.ErrStale) || errors.Is(err, workbench.ErrInvalid) || errors.Is(err, workbench.ErrNotFound) || errors.Is(err, workbench.ErrPrivate) {
		return river.JobCancel(errors.New("collection envelope is stale or unauthorized"))
	}
	if err != nil {
		if job.Attempt >= job.MaxAttempts {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			// If the database itself is unavailable this is best effort.
			// Never log SQL or source data.
			_ = w.Service.FailCollection(cleanup, job.Args)
		}
		return errors.New("supplier collection temporarily unavailable")
	}
	return nil
}
