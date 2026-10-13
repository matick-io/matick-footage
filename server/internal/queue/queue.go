// Package queue is the job table: enqueue stages for a recording, claim the
// next one with FOR UPDATE SKIP LOCKED, and record how it ended. Workers in
// other languages use the same SQL; see workers/footage_worker/queue.py.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MaxAttempts is how often a failing job runs before it stays failed.
const MaxAttempts = 3

// StaleAfter is how long a running job may go without a heartbeat before it's
// handed to another worker.
const StaleAfter = 5 * time.Minute

type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Spec is one stage to run for a recording.
type Spec struct {
	Stage    string
	Version  int
	Model    string
	InputKey string
	// After names a stage that must be done for the same input key first.
	After string
}

// Key identifies a stage implementation, e.g. "events:1". Workers claim only
// keys they implement, so a version bump waits for an updated worker.
func (s Spec) Key() string { return fmt.Sprintf("%s:%d", s.Stage, s.Version) }

// Enqueue adds jobs that don't exist yet. Existing ones, in any state, are left
// alone, which makes rescans idempotent.
func Enqueue(ctx context.Context, db DB, recordingID int64, specs ...Spec) error {
	for _, s := range specs {
		var after *string
		if s.After != "" {
			after = &s.After
		}
		_, err := db.Exec(ctx, `
			INSERT INTO jobs (recording_id, stage, version, model, input_key, after)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (recording_id, stage, version, model, input_key) DO NOTHING`,
			recordingID, s.Stage, s.Version, s.Model, s.InputKey, after)
		if err != nil {
			return fmt.Errorf("enqueue %s: %w", s.Stage, err)
		}
	}
	return nil
}

type Job struct {
	ID          int64
	RecordingID int64
	Stage       string
	Version     int
	Model       string
	InputKey    string
	Attempts    int
}

// ErrEmpty means no job is ready.
var ErrEmpty = errors.New("no job ready")

// Claim takes the oldest ready job whose stage:version is in keys.
func Claim(ctx context.Context, db DB, keys []string, worker string) (*Job, error) {
	j := &Job{}
	err := db.QueryRow(ctx, `
		UPDATE jobs SET status = 'running', attempts = attempts + 1, worker = $2,
		       started_at = now(), heartbeat_at = now(), error = NULL
		WHERE id = (
			SELECT j.id FROM jobs j
			WHERE j.status = 'queued' AND j.run_after <= now()
			  AND j.stage || ':' || j.version = ANY ($1)
			  AND (j.after IS NULL OR EXISTS (
			        SELECT 1 FROM jobs p
			        WHERE p.recording_id = j.recording_id AND p.stage = j.after
			          AND p.input_key = j.input_key AND p.status = 'done'))
			ORDER BY j.created_at, j.id
			LIMIT 1
			FOR UPDATE SKIP LOCKED)
		RETURNING id, recording_id, stage, version, model, input_key, attempts`,
		keys, worker,
	).Scan(&j.ID, &j.RecordingID, &j.Stage, &j.Version, &j.Model, &j.InputKey, &j.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEmpty
	}
	return j, err
}

func Complete(ctx context.Context, db DB, id int64, result any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		UPDATE jobs SET status = 'done', result = $2, finished_at = now(), error = NULL
		WHERE id = $1`, id, data)
	return err
}

// Fail requeues the job with a backoff, or marks it failed after MaxAttempts.
func Fail(ctx context.Context, db DB, id int64, cause error) error {
	_, err := db.Exec(ctx, `
		UPDATE jobs SET
		  status = CASE WHEN attempts < $3 THEN 'queued' ELSE 'failed' END,
		  run_after = now() + attempts * interval '1 minute',
		  finished_at = CASE WHEN attempts < $3 THEN NULL ELSE now() END,
		  error = $2
		WHERE id = $1`, id, cause.Error(), MaxAttempts)
	return err
}

// RequeueStale hands running jobs whose worker went quiet back to the queue.
func RequeueStale(ctx context.Context, db DB) (int64, error) {
	tag, err := db.Exec(ctx, `
		UPDATE jobs SET status = 'queued', error = 'worker stopped responding'
		WHERE status = 'running' AND heartbeat_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(StaleAfter.Seconds())))
	return tag.RowsAffected(), err
}

// Retry queues a finished or failed job's stage again for its current input.
func Retry(ctx context.Context, db DB, recordingID int64, stage string) (int64, error) {
	tag, err := db.Exec(ctx, `
		UPDATE jobs SET status = 'queued', attempts = 0, run_after = now(), error = NULL,
		       result = NULL, finished_at = NULL
		WHERE id = (
			SELECT id FROM jobs WHERE recording_id = $1 AND stage = $2
			ORDER BY created_at DESC, id DESC LIMIT 1)
		  AND status IN ('done', 'failed')`, recordingID, stage)
	return tag.RowsAffected(), err
}
