// Package scan finds recordings in every source and queues their stages.
// It rescans on an interval rather than relying on file events, which are
// unreliable over SMB and NFS and don't exist for buckets.
package scan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"gocloud.dev/blob"

	"github.com/matick-io/matick-footage/server/internal/pipeline"
	"github.com/matick-io/matick-footage/server/internal/queue"
	"github.com/matick-io/matick-footage/server/internal/sidecar"
	"github.com/matick-io/matick-footage/server/internal/storage"
)

var videoExts = map[string]bool{
	".mp4": true, ".mov": true, ".mkv": true, ".m4v": true,
	".webm": true, ".avi": true, ".mxf": true,
}

// IsVideo reports whether a file name has a video extension the scanner indexes.
func IsVideo(name string) bool { return videoExts[strings.ToLower(path.Ext(name))] }

// File is a recording or sidecar as its source reports it.
type File struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// key changes whenever the file does. Postgres keeps microseconds, so the
// stored mtime gives the same key.
func (f File) key() string {
	return fmt.Sprintf("%d-%d", f.Size, f.ModTime.Truncate(time.Microsecond).UnixMicro())
}

// Ingest records a recording and queues the stages its file and sidecar need.
// Stages already queued or done for the same input are left alone, so calling
// it again is harmless.
func Ingest(ctx context.Context, db *pgxpool.Pool, source string, video File, sc *File,
	transcribeModel string) (int64, error) {
	var sidecarPath, sidecarKey string
	if sc != nil {
		sidecarPath, sidecarKey = sc.Path, sc.key()
	}
	var id int64
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		mtime := video.ModTime.Truncate(time.Microsecond)
		err := tx.QueryRow(ctx, `
			INSERT INTO recordings (source, path, size, mtime, recorded_at, sidecar_path, sidecar_key)
			VALUES ($1, $2, $3, $4, $4, nullif($5, ''), nullif($6, ''))
			ON CONFLICT (source, path) DO UPDATE SET
			  size = excluded.size, mtime = excluded.mtime,
			  sidecar_path = excluded.sidecar_path, sidecar_key = excluded.sidecar_key,
			  missing_since = NULL, updated_at = now()
			RETURNING id`, source, video.Path, video.Size, mtime, sidecarPath, sidecarKey).Scan(&id)
		if err != nil {
			return err
		}
		return queue.Enqueue(ctx, tx, id, pipeline.Specs(video.key(), sidecarKey, transcribeModel)...)
	})
	return id, err
}

// QueueStages queues whatever stages known recordings lack, such as a stage
// whose version was bumped or a new transcription model. The scanner only
// queues for new or changed files, so this runs once at startup.
func QueueStages(ctx context.Context, db *pgxpool.Pool, transcribeModel string) error {
	rows, err := db.Query(ctx, `
		SELECT id, size, mtime, coalesce(sidecar_key, '') FROM recordings WHERE missing_since IS NULL`)
	if err != nil {
		return err
	}
	type rec struct {
		id         int64
		file       File
		sidecarKey string
	}
	recs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (rec, error) {
		var r rec
		err := row.Scan(&r.id, &r.file.Size, &r.file.ModTime, &r.sidecarKey)
		return r, err
	})
	if err != nil {
		return err
	}
	for _, r := range recs {
		if err := queue.Enqueue(ctx, db, r.id, pipeline.Specs(r.file.key(), r.sidecarKey, transcribeModel)...); err != nil {
			return err
		}
	}
	return nil
}

type Scanner struct {
	DB              *pgxpool.Pool
	Sources         []*storage.Source
	StableFor       time.Duration
	TranscribeModel string

	// pending tracks files that changed recently, until they settle.
	pending map[pendingKey]observation
	now     func() time.Time
}

type pendingKey struct{ source, path string }

type observation struct {
	key   string
	since time.Time
}

type known struct {
	id         int64
	fileKey    string
	sidecarKey string
	missing    bool
}

func (s *Scanner) Run(ctx context.Context, interval time.Duration) {
	if err := QueueStages(ctx, s.DB, s.TranscribeModel); err != nil {
		slog.Error("queue stages", "err", err)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.Scan(ctx); err != nil {
			slog.Error("scan", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Scan walks every source once. A failing source doesn't stop the others.
func (s *Scanner) Scan(ctx context.Context) error {
	if s.pending == nil {
		s.pending = map[pendingKey]observation{}
	}
	if s.now == nil {
		s.now = time.Now
	}
	var errs []error
	for _, src := range s.Sources {
		if err := s.scan(ctx, src); err != nil {
			errs = append(errs, fmt.Errorf("source %s: %w", src.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Scanner) scan(ctx context.Context, src *storage.Source) error {
	have, err := s.load(ctx, src.Name)
	if err != nil {
		return err
	}
	var videos []File
	sidecars := map[string]File{}
	err = src.Walk(ctx, func(obj *blob.ListObject) error {
		f := File{Path: obj.Key, Size: obj.Size, ModTime: obj.ModTime}
		switch {
		case strings.HasSuffix(obj.Key, sidecar.Suffix):
			sidecars[obj.Key] = f
		case IsVideo(obj.Key):
			videos = append(videos, f)
		}
		return nil
	})
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var ingested int
	for _, v := range videos {
		seen[v.Path] = true
		var sc *File
		if f, ok := sidecars[strings.TrimSuffix(v.Path, path.Ext(v.Path))+sidecar.Suffix]; ok {
			sc = &f
		}
		changed, err := s.visit(ctx, src.Name, v, sc, have[v.Path])
		if err != nil {
			return fmt.Errorf("%s: %w", v.Path, err)
		}
		if changed {
			ingested++
		}
	}
	for k := range s.pending {
		if k.source == src.Name && !seen[k.path] {
			delete(s.pending, k)
		}
	}
	if err := s.markMissing(ctx, src.Name, have, seen); err != nil {
		return err
	}
	if ingested > 0 {
		slog.Info("scan", "source", src.Name, "recordings", len(seen), "ingested", ingested)
	}
	return nil
}

func (s *Scanner) visit(ctx context.Context, source string, v File, sc *File, k *known) (bool, error) {
	fileKey, sidecarKey := v.key(), ""
	if sc != nil {
		sidecarKey = sc.key()
	}
	pk := pendingKey{source, v.Path}
	if k != nil && k.fileKey == fileKey {
		delete(s.pending, pk)
		if k.sidecarKey == sidecarKey && !k.missing {
			return false, nil
		}
	} else {
		// New or changed: wait until the size and mtime stop moving.
		p, ok := s.pending[pk]
		if !ok || p.key != fileKey {
			s.pending[pk] = observation{key: fileKey, since: s.now()}
			return false, nil
		}
		if s.now().Sub(p.since) < s.StableFor {
			return false, nil
		}
		delete(s.pending, pk)
	}
	_, err := Ingest(ctx, s.DB, source, v, sc, s.TranscribeModel)
	return true, err
}

func (s *Scanner) load(ctx context.Context, source string) (map[string]*known, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, path, size, mtime, coalesce(sidecar_key, ''), missing_since IS NOT NULL
		FROM recordings WHERE source = $1`, source)
	if err != nil {
		return nil, err
	}
	have := map[string]*known{}
	for rows.Next() {
		var (
			k known
			f File
		)
		if err := rows.Scan(&k.id, &f.Path, &f.Size, &f.ModTime, &k.sidecarKey, &k.missing); err != nil {
			return nil, err
		}
		k.fileKey = f.key()
		have[f.Path] = &k
	}
	return have, rows.Err()
}

// markMissing flags recordings whose file is gone. Nothing is deleted: a share
// that failed to mount must not wipe the index.
func (s *Scanner) markMissing(ctx context.Context, source string, have map[string]*known, seen map[string]bool) error {
	if len(seen) == 0 && len(have) > 0 {
		slog.Warn("scan found no recordings; is the source mounted?", "source", source)
		return nil
	}
	var gone []int64
	for p, k := range have {
		if !seen[p] && !k.missing {
			gone = append(gone, k.id)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	_, err := s.DB.Exec(ctx, `UPDATE recordings SET missing_since = now() WHERE id = ANY ($1)`, gone)
	return err
}
