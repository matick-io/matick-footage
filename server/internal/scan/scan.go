// Package scan finds recordings under the footage root and queues their stages.
// It rescans on an interval rather than relying on file events, which are
// unreliable over SMB and NFS.
package scan

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matick-io/matick-footage/server/internal/pipeline"
	"github.com/matick-io/matick-footage/server/internal/queue"
	"github.com/matick-io/matick-footage/server/internal/sidecar"
)

var videoExts = map[string]bool{
	".mp4": true, ".mov": true, ".mkv": true, ".m4v": true,
	".webm": true, ".avi": true, ".mxf": true,
}

type Scanner struct {
	DB              *pgxpool.Pool
	Root            string
	StableFor       time.Duration
	TranscribeModel string

	// pending tracks files that changed recently, until they settle.
	pending map[string]observation
	now     func() time.Time
}

type observation struct {
	size  int64
	mtime time.Time
	since time.Time
}

type known struct {
	id         int64
	fileKey    string
	sidecarKey string
	missing    bool
}

func (s *Scanner) Run(ctx context.Context, interval time.Duration) {
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

// Scan walks the root once.
func (s *Scanner) Scan(ctx context.Context) error {
	if s.pending == nil {
		s.pending = map[string]observation{}
	}
	if s.now == nil {
		s.now = time.Now
	}
	have, err := s.load(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var ingested int
	err = filepath.WalkDir(s.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("scan: skipping", "path", path, "err", err)
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && path != s.Root {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !videoExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		rel, err := filepath.Rel(s.Root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		info, err := d.Info()
		if err != nil {
			return nil
		}
		changed, err := s.visit(ctx, rel, info, have[rel])
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if changed {
			ingested++
		}
		return nil
	})
	if err != nil {
		return err
	}
	for rel := range s.pending {
		if !seen[rel] {
			delete(s.pending, rel)
		}
	}
	if err := s.markMissing(ctx, have, seen); err != nil {
		return err
	}
	if ingested > 0 {
		slog.Info("scan", "recordings", len(seen), "ingested", ingested)
	}
	return nil
}

func (s *Scanner) visit(ctx context.Context, rel string, info fs.FileInfo, k *known) (bool, error) {
	// Postgres keeps microseconds; truncate so the stored mtime compares equal.
	size, mtime := info.Size(), info.ModTime().Truncate(time.Microsecond)
	fileKey := statKey(size, mtime)
	sidecarRel, sidecarKey := s.findSidecar(rel)

	if k != nil && k.fileKey == fileKey {
		delete(s.pending, rel)
		if k.sidecarKey == sidecarKey && !k.missing {
			return false, nil
		}
	} else {
		// New or changed: wait until the size and mtime stop moving.
		p, ok := s.pending[rel]
		if !ok || p.size != size || !p.mtime.Equal(mtime) {
			s.pending[rel] = observation{size: size, mtime: mtime, since: s.now()}
			return false, nil
		}
		if s.now().Sub(p.since) < s.StableFor {
			return false, nil
		}
		delete(s.pending, rel)
	}
	return true, s.ingest(ctx, rel, size, mtime, fileKey, sidecarRel, sidecarKey)
}

func (s *Scanner) ingest(ctx context.Context, rel string, size int64, mtime time.Time,
	fileKey, sidecarRel, sidecarKey string) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `
			INSERT INTO recordings (path, size, mtime, recorded_at, sidecar_path, sidecar_key)
			VALUES ($1, $2, $3, $3, nullif($4, ''), nullif($5, ''))
			ON CONFLICT (path) DO UPDATE SET
			  size = excluded.size, mtime = excluded.mtime,
			  sidecar_path = excluded.sidecar_path, sidecar_key = excluded.sidecar_key,
			  missing_since = NULL, updated_at = now()
			RETURNING id`, rel, size, mtime, sidecarRel, sidecarKey).Scan(&id)
		if err != nil {
			return err
		}
		return queue.Enqueue(ctx, tx, id, pipeline.Specs(fileKey, sidecarKey, s.TranscribeModel)...)
	})
}

// findSidecar returns the sidecar's path relative to the root and a key that
// changes when it's rewritten, or empty strings when there's none.
func (s *Scanner) findSidecar(rel string) (string, string) {
	sc := strings.TrimSuffix(rel, filepath.Ext(rel)) + sidecar.Suffix
	info, err := os.Stat(filepath.Join(s.Root, filepath.FromSlash(sc)))
	if err != nil || info.IsDir() {
		return "", ""
	}
	return sc, statKey(info.Size(), info.ModTime())
}

func (s *Scanner) load(ctx context.Context) (map[string]*known, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, path, size, mtime, coalesce(sidecar_key, ''), missing_since IS NOT NULL
		FROM recordings`)
	if err != nil {
		return nil, err
	}
	have := map[string]*known{}
	for rows.Next() {
		var (
			k     known
			path  string
			size  int64
			mtime time.Time
		)
		if err := rows.Scan(&k.id, &path, &size, &mtime, &k.sidecarKey, &k.missing); err != nil {
			return nil, err
		}
		k.fileKey = statKey(size, mtime)
		have[path] = &k
	}
	return have, rows.Err()
}

// markMissing flags recordings whose file is gone. Nothing is deleted: a share
// that failed to mount must not wipe the index.
func (s *Scanner) markMissing(ctx context.Context, have map[string]*known, seen map[string]bool) error {
	if len(seen) == 0 && len(have) > 0 {
		slog.Warn("scan found no recordings; is the footage share mounted?", "root", s.Root)
		return nil
	}
	var gone []int64
	for path, k := range have {
		if !seen[path] && !k.missing {
			gone = append(gone, k.id)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	_, err := s.DB.Exec(ctx, `UPDATE recordings SET missing_since = now() WHERE id = ANY ($1)`, gone)
	return err
}

func statKey(size int64, mtime time.Time) string {
	return fmt.Sprintf("%d-%d", size, mtime.Truncate(time.Microsecond).UnixMicro())
}
