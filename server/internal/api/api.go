// Package api serves /api/v1 and the built web UI. The UI, the MCP server and
// scripts all use the same endpoints; none gets special access.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matick-io/matick-footage/server/internal/config"
	"github.com/matick-io/matick-footage/server/internal/storage"
)

type Server struct {
	DB      *pgxpool.Pool
	Cfg     config.Config
	Sources *storage.Set
	// Version and Commit identify the build, shown in the UI.
	Version, Commit string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/config", s.config)
	mux.HandleFunc("GET /api/v1/search", s.search)
	mux.HandleFunc("GET /api/v1/facets", s.facets)
	mux.HandleFunc("GET /api/v1/jobs", s.jobs)

	mux.HandleFunc("GET /api/v1/recordings", s.listRecordings)
	mux.HandleFunc("GET /api/v1/recordings/{id}", s.getRecording)
	mux.HandleFunc("DELETE /api/v1/recordings/{id}", s.deleteRecording)
	mux.HandleFunc("GET /api/v1/recordings/{id}/original", s.recordingOriginal)
	mux.HandleFunc("PUT /api/v1/recordings/{id}/sidecar", s.uploadSidecar)
	mux.HandleFunc("GET /api/v1/recordings/{id}/events", s.recordingEvents)
	mux.HandleFunc("GET /api/v1/recordings/{id}/transcript", s.recordingTranscript)
	mux.HandleFunc("GET /api/v1/recordings/{id}/subtitles/{file}", s.recordingSubtitles)
	mux.HandleFunc("GET /api/v1/recordings/{id}/proxy", s.recordingProxy)
	mux.HandleFunc("GET /api/v1/recordings/{id}/sprites/{sheet}", s.recordingSprite)
	mux.HandleFunc("POST /api/v1/recordings/{id}/reprocess", s.reprocess)
	mux.HandleFunc("PUT /api/v1/uploads/{name}", s.uploadRecording)

	mux.HandleFunc("GET /api/v1/selects", s.listSelects)
	mux.HandleFunc("POST /api/v1/selects", s.createSelect)
	mux.HandleFunc("GET /api/v1/selects/{id}", s.getSelect)
	mux.HandleFunc("PATCH /api/v1/selects/{id}", s.updateSelect)
	mux.HandleFunc("DELETE /api/v1/selects/{id}", s.deleteSelect)
	mux.HandleFunc("POST /api/v1/selects/{id}/items", s.addSelectItem)
	mux.HandleFunc("PUT /api/v1/selects/{id}/items", s.replaceSelectItems)
	mux.HandleFunc("GET /api/v1/selects/{id}/export", s.exportSelect)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "no such endpoint")
	})
	if s.Cfg.WebDir != "" {
		mux.Handle("/", spa(s.Cfg.WebDir))
	}
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Ping(r.Context()); err != nil {
		fail(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	targets := []map[string]string{}
	for _, t := range s.Cfg.ExportTargets {
		targets = append(targets, map[string]string{"name": t.Name})
	}
	writeJSON(w, map[string]any{
		"export_targets":   targets,
		"transcribe_model": s.Cfg.TranscribeModel,
		"upload_max":       s.Cfg.UploadMax,
		"version":          s.Version,
		"commit":           s.Commit,
	})
}

// spa serves the built UI, falling back to index.html for client-side routes.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write response", "err", err)
	}
}

func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// failErr maps a database error to a response, hiding internals.
func failErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	slog.Error("request failed", "path", r.URL.Path, "err", err)
	fail(w, http.StatusInternalServerError, "internal error")
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusBadRequest, name+" must be a positive integer")
		return 0, false
	}
	return id, true
}
