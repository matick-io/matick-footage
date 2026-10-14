package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/matick-io/matick-footage/server/internal/queue"
	"github.com/matick-io/matick-footage/server/internal/sidecar"
)

type eventsResult struct {
	Entries  int      `json:"entries"`
	Skipped  []string `json:"skipped,omitempty"`
	Session  string   `json:"session"`
	SyncMode string   `json:"sync_mode,omitempty"`
}

// events loads a recording's sidecar, replacing whatever an earlier version of
// the sidecar produced.
func (w *Worker) events(ctx context.Context, job *queue.Job) (*eventsResult, error) {
	var (
		source      string
		sidecarPath *string
	)
	if err := w.DB.QueryRow(ctx, `SELECT source, sidecar_path FROM recordings WHERE id = $1`,
		job.RecordingID).Scan(&source, &sidecarPath); err != nil {
		return nil, err
	}
	if sidecarPath == nil {
		return nil, errors.New("recording has no sidecar")
	}
	src, ok := w.Sources.Get(source)
	if !ok {
		return nil, fmt.Errorf("source %q is not configured", source)
	}
	fh, err := src.Bucket.NewReader(ctx, *sidecarPath, nil)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	f, err := sidecar.Parse(fh)
	if err != nil {
		return nil, err
	}
	res := &eventsResult{Entries: len(f.Entries), Session: f.Session.SessionID, SyncMode: f.Session.Sync}
	for _, p := range f.Problems {
		res.Skipped = append(res.Skipped, p.String())
	}

	err = pgx.BeginFunc(ctx, w.DB, func(tx pgx.Tx) error {
		s := f.Session
		var sessionID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO sessions (project, session_key, engine, build, platform, tester, header)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (project, session_key) DO UPDATE SET
			  engine = excluded.engine, build = excluded.build, platform = excluded.platform,
			  tester = excluded.tester, header = excluded.header
			RETURNING id`,
			s.Project, s.SessionID, s.Engine, s.Build, s.Platform, s.Tester, s.Raw,
		).Scan(&sessionID); err != nil {
			return fmt.Errorf("session: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE recordings SET session_id = $2, sync_mode = $3,
			  recorded_at = coalesce($4, recorded_at), updated_at = now()
			WHERE id = $1`, job.RecordingID, sessionID, s.Sync, s.StartedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM events WHERE recording_id = $1`, job.RecordingID); err != nil {
			return err
		}
		rows := make([][]any, 0, len(f.Entries))
		for _, e := range f.Entries {
			data := e.Data
			if data == nil {
				data = []byte("{}")
			}
			tags := e.Tags
			if tags == nil {
				tags = []string{}
			}
			rows = append(rows, []any{
				job.RecordingID, e.Type, e.T, e.Dur, e.Kind, e.Label, e.Source, tags,
				int16(e.Importance), data, searchText(e),
			})
		}
		// COPY can't call to_tsvector, so rows land in a temp table first.
		if _, err := tx.Exec(ctx, `
			CREATE TEMP TABLE incoming_events (
			  recording_id bigint, type text, t double precision, dur double precision,
			  kind text, label text, source text, tags text[], importance smallint,
			  data jsonb, search_text text) ON COMMIT DROP`); err != nil {
			return err
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"incoming_events"},
			[]string{"recording_id", "type", "t", "dur", "kind", "label", "source", "tags",
				"importance", "data", "search_text"},
			pgx.CopyFromRows(rows)); err != nil {
			return fmt.Errorf("copy events: %w", err)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO events (recording_id, type, t, dur, kind, label, source, tags, importance, data, search)
			SELECT recording_id, type, t, dur, kind, label, source, tags, importance, data,
			       to_tsvector('simple', search_text)
			FROM incoming_events`)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// searchText is what free-text search matches for an event: its label, the
// words of its kind, and its tags.
func searchText(e sidecar.Entry) string {
	parts := []string{e.Label, strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(e.Kind)}
	parts = append(parts, e.Tags...)
	return strings.Join(parts, " ")
}
