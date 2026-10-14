package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"unicode"

	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"

	"github.com/matick-io/matick-footage/server/internal/scan"
	"github.com/matick-io/matick-footage/server/internal/sidecar"
)

const (
	// freeReserve is what an upload must leave free on the library's disk.
	freeReserve = 1 << 30
	sidecarMax  = 256 << 20
)

// uploadRecording stores a video in the library under uploads/<date>/ and
// indexes it straight away, without waiting for the scanner. The body is the
// file itself, so `curl -T clip.mp4 …/api/v1/uploads/clip.mp4` works.
func (s *Server) uploadRecording(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validName(name) || !scan.IsVideo(name) {
		fail(w, http.StatusBadRequest, "name must be a video file name, such as session.mp4")
		return
	}
	switch size := r.ContentLength; {
	case size < 0:
		fail(w, http.StatusLengthRequired, "Content-Length is required")
		return
	case size == 0:
		fail(w, http.StatusBadRequest, "the file is empty")
		return
	case size > s.Cfg.UploadMax:
		fail(w, http.StatusRequestEntityTooLarge, "files are limited to "+sizeText(s.Cfg.UploadMax))
		return
	}
	lib := s.Sources.Library
	if free, ok := lib.Free(); ok && free < uint64(r.ContentLength)+freeReserve {
		fail(w, http.StatusInsufficientStorage, "not enough free space in the library")
		return
	}
	ctx := r.Context()
	key, err := freeKey(ctx, lib.Bucket, path.Join("uploads", time.Now().Format(time.DateOnly), name))
	if err != nil {
		failErr(w, r, err)
		return
	}
	opts := &blob.WriterOptions{ContentType: r.Header.Get("Content-Type"), IfNotExist: true}
	if err := write(ctx, lib.Bucket, key, r.Body, r.ContentLength, opts); err != nil {
		if gcerrors.Code(err) == gcerrors.FailedPrecondition {
			fail(w, http.StatusConflict, "a file with this name was uploaded at the same time; try again")
			return
		}
		slog.Warn("upload failed", "key", key, "err", err)
		fail(w, http.StatusBadRequest, "upload did not complete")
		return
	}
	attrs, err := lib.Bucket.Attributes(ctx, key)
	if err != nil {
		failErr(w, r, err)
		return
	}
	id, err := scan.Ingest(ctx, s.DB, lib.Name,
		scan.File{Path: key, Size: attrs.Size, ModTime: attrs.ModTime}, nil, s.Cfg.TranscribeModel)
	if err != nil {
		failErr(w, r, err)
		return
	}
	slog.Info("uploaded", "key", key, "bytes", attrs.Size, "recording", id)
	s.writeRecording(w, r, id, http.StatusCreated)
}

// uploadSidecar stores a library recording's event sidecar next to it and
// loads it, replacing any earlier one.
func (s *Server) uploadSidecar(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var (
		source string
		video  scan.File
	)
	if err := s.DB.QueryRow(r.Context(), `SELECT source, path, size, mtime FROM recordings WHERE id = $1`, id).
		Scan(&source, &video.Path, &video.Size, &video.ModTime); err != nil {
		failErr(w, r, err)
		return
	}
	lib := s.Sources.Library
	if source != lib.Name {
		fail(w, http.StatusConflict, "only library recordings take an uploaded sidecar; put it next to the file instead")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, sidecarMax))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "sidecars are limited to "+sizeText(sidecarMax))
			return
		}
		fail(w, http.StatusBadRequest, "upload did not complete")
		return
	}
	if _, err := sidecar.Parse(bytes.NewReader(data)); err != nil {
		fail(w, http.StatusBadRequest, "not a usable sidecar: "+err.Error())
		return
	}
	key := strings.TrimSuffix(video.Path, path.Ext(video.Path)) + sidecar.Suffix
	ctx := r.Context()
	if err := lib.Bucket.WriteAll(ctx, key, data, &blob.WriterOptions{ContentType: "application/jsonl"}); err != nil {
		failErr(w, r, err)
		return
	}
	attrs, err := lib.Bucket.Attributes(ctx, key)
	if err != nil {
		failErr(w, r, err)
		return
	}
	sc := &scan.File{Path: key, Size: attrs.Size, ModTime: attrs.ModTime}
	if _, err := scan.Ingest(ctx, s.DB, lib.Name, video, sc, s.Cfg.TranscribeModel); err != nil {
		failErr(w, r, err)
		return
	}
	s.writeRecording(w, r, id, http.StatusOK)
}

// deleteRecording removes a library recording: its file and sidecar, what was
// indexed from them, and its proxy and thumbnails. Recordings in watched
// sources belong to whatever wrote them and are never deleted.
func (s *Server) deleteRecording(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var (
		source, rel string
		sidecarPath *string
	)
	if err := s.DB.QueryRow(r.Context(), `SELECT source, path, sidecar_path FROM recordings WHERE id = $1`, id).
		Scan(&source, &rel, &sidecarPath); err != nil {
		failErr(w, r, err)
		return
	}
	lib := s.Sources.Library
	if source != lib.Name {
		fail(w, http.StatusConflict, "only library recordings can be deleted; this one is in "+source)
		return
	}
	keys := []string{rel}
	if sidecarPath != nil {
		keys = append(keys, *sidecarPath)
	}
	// Files first: if this stops halfway, the scanner marks the recording missing
	// rather than finding a file the index forgot.
	for _, key := range keys {
		if err := lib.Bucket.Delete(r.Context(), key); err != nil && gcerrors.Code(err) != gcerrors.NotFound {
			failErr(w, r, err)
			return
		}
	}
	if _, err := s.DB.Exec(r.Context(), `DELETE FROM recordings WHERE id = $1`, id); err != nil {
		failErr(w, r, err)
		return
	}
	// Proxies and thumbnails are rebuilt from the original, so a leftover is
	// only wasted space.
	for _, rel := range []string{fmt.Sprintf("proxies/%d.mp4", id), fmt.Sprintf("sprites/%d", id)} {
		if p, ok := s.dataPath(rel); ok {
			if err := os.RemoveAll(p); err != nil {
				slog.Warn("remove derived files", "path", p, "err", err)
			}
		}
	}
	slog.Info("deleted", "recording", id, "key", rel)
	w.WriteHeader(http.StatusNoContent)
}

// recordingOriginal says where to read the original file: a file:// URL on the
// mount the core and workers share, or a signed link for other storage.
func (s *Server) recordingOriginal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var source, rel string
	if err := s.DB.QueryRow(r.Context(), `SELECT source, path FROM recordings WHERE id = $1`, id).
		Scan(&source, &rel); err != nil {
		failErr(w, r, err)
		return
	}
	src, ok := s.Sources.Get(source)
	if !ok {
		fail(w, http.StatusConflict, "source "+source+" is not configured")
		return
	}
	u, err := src.URL(r.Context(), rel)
	if err != nil {
		failErr(w, r, err)
		return
	}
	writeJSON(w, map[string]string{"url": u})
}

func (s *Server) writeRecording(w http.ResponseWriter, r *http.Request, id int64, status int) {
	recs, err := s.loadRecordings(r.Context(), []int64{id})
	if err != nil {
		failErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/api/v1/recordings/%d", id))
	w.WriteHeader(status)
	writeJSON(w, recs[id])
}

// sizeText writes a byte count in the largest whole binary unit, e.g. 50 GiB.
func sizeText(n int64) string {
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for i := len(units) - 1; i >= 0; i-- {
		unit := int64(1) << (10 * (i + 1))
		if n >= unit && n%unit == 0 {
			return fmt.Sprintf("%d %s", n/unit, units[i])
		}
	}
	return fmt.Sprintf("%d bytes", n)
}

// validName accepts a plain file name: no folders, not hidden, no control characters.
func validName(name string) bool {
	return name != "" && len(name) <= 255 && !strings.ContainsAny(name, `/\`) &&
		!strings.HasPrefix(name, ".") && strings.IndexFunc(name, unicode.IsControl) < 0
}

// freeKey returns key, or key with " (2)", " (3)"… before the extension when
// that name is taken.
func freeKey(ctx context.Context, b *blob.Bucket, key string) (string, error) {
	ext := path.Ext(key)
	stem := strings.TrimSuffix(key, ext)
	for n := 1; n <= 1000; n++ {
		k := key
		if n > 1 {
			k = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		taken, err := b.Exists(ctx, k)
		if err != nil {
			return "", err
		}
		if !taken {
			return k, nil
		}
	}
	return "", fmt.Errorf("no free name for %s", key)
}

// write streams exactly size bytes into key. On any failure the write is
// cancelled, which leaves no file behind.
func write(ctx context.Context, b *blob.Bucket, key string, body io.Reader, size int64, opts *blob.WriterOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wr, err := b.NewWriter(ctx, key, opts)
	if err != nil {
		return err
	}
	n, err := io.Copy(wr, io.LimitReader(body, size))
	if err == nil && n != size {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		cancel()
		_ = wr.Close()
		return err
	}
	return wr.Close()
}
