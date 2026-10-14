// Package pipeline defines the ingest stages and runs the ones the core owns.
// Media and ML stages run in the Python workers, which claim the same jobs.
package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matick-io/matick-footage/server/internal/queue"
	"github.com/matick-io/matick-footage/server/internal/storage"
)

// Stage versions. Bumping one re-runs that stage for every recording.
const (
	ProbeVersion      = 1
	ProxyVersion      = 1
	TranscribeVersion = 1
	EventsVersion     = 1
)

// Specs lists the stages a recording needs for its current file and sidecar.
// fileKey and sidecarKey change whenever the file or sidecar does.
func Specs(fileKey, sidecarKey, transcribeModel string) []queue.Spec {
	specs := []queue.Spec{
		{Stage: "probe", Version: ProbeVersion, InputKey: fileKey},
		{Stage: "proxy", Version: ProxyVersion, InputKey: fileKey, After: "probe"},
	}
	if transcribeModel != "off" {
		specs = append(specs, queue.Spec{
			Stage: "transcribe", Version: TranscribeVersion, Model: transcribeModel,
			InputKey: fileKey, After: "probe",
		})
	}
	if sidecarKey != "" {
		specs = append(specs, queue.Spec{Stage: "events", Version: EventsVersion, InputKey: sidecarKey})
	}
	return specs
}

// Worker runs the core's own stages.
type Worker struct {
	DB      *pgxpool.Pool
	Sources *storage.Set
}

func (w *Worker) Run(ctx context.Context) {
	keys := []string{queue.Spec{Stage: "events", Version: EventsVersion}.Key()}
	name, _ := os.Hostname()
	name = "core@" + name
	idle := time.NewTicker(time.Second)
	defer idle.Stop()
	maintenance := time.NewTicker(time.Minute)
	defer maintenance.Stop()
	for {
		job, err := queue.Claim(ctx, w.DB, keys, name)
		switch {
		case err == nil:
			w.run(ctx, job)
			continue
		case !errors.Is(err, queue.ErrEmpty):
			slog.Error("claim job", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-maintenance.C:
			if n, err := queue.RequeueStale(ctx, w.DB); err != nil {
				slog.Error("requeue stale jobs", "err", err)
			} else if n > 0 {
				slog.Warn("requeued stale jobs", "count", n)
			}
		case <-idle.C:
		}
	}
}

func (w *Worker) run(ctx context.Context, job *queue.Job) {
	log := slog.With("job", job.ID, "stage", job.Stage, "recording", job.RecordingID)
	var (
		result any
		err    error
	)
	switch job.Stage {
	case "events":
		result, err = w.events(ctx, job)
	default:
		err = errors.New("core does not run stage " + job.Stage)
	}
	if err != nil {
		log.Error("stage failed", "err", err)
		if ferr := queue.Fail(ctx, w.DB, job.ID, err); ferr != nil {
			log.Error("record failure", "err", ferr)
		}
		return
	}
	log.Info("stage done", "result", result)
	if cerr := queue.Complete(ctx, w.DB, job.ID, result); cerr != nil {
		log.Error("record completion", "err", cerr)
	}
}
