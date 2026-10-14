package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/matick-io/matick-footage/server/internal/config"
	"github.com/matick-io/matick-footage/server/internal/queue"
)

type Session struct {
	Project   string `json:"project"`
	SessionID string `json:"session_id"`
	Engine    string `json:"engine"`
	Build     string `json:"build"`
	Platform  string `json:"platform"`
	Tester    string `json:"tester"`
}

// Sprite describes the thumbnail sheets for hover-scrub. Tile i shows time
// i*Interval and sits in sheet i/(Cols*Rows), 1-based in the URL.
type Sprite struct {
	Interval float64 `json:"interval"`
	Cols     int     `json:"cols"`
	Rows     int     `json:"rows"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Count    int     `json:"count"`
	Sheets   int     `json:"sheets"`
	URL      string  `json:"url"` // with {sheet} placeholder
}

type Stage struct {
	Status   string     `json:"status"`
	Version  int        `json:"version"`
	Model    string     `json:"model,omitempty"`
	Attempts int        `json:"attempts"`
	Error    string     `json:"error,omitempty"`
	Finished *time.Time `json:"finished_at,omitempty"`
}

type Recording struct {
	ID int64 `json:"id"`
	// Source names where the file lives; Path is relative to it.
	Source string `json:"source"`
	// Library recordings are the app's own: they can be deleted or given a sidecar.
	Library     bool             `json:"library"`
	Path        string           `json:"path"`
	Name        string           `json:"name"`
	Size        int64            `json:"size"`
	RecordedAt  time.Time        `json:"recorded_at"`
	Duration    *float64         `json:"duration"`
	FPS         *float64         `json:"fps"`
	Width       *int             `json:"width"`
	Height      *int             `json:"height"`
	VideoCodec  *string          `json:"video_codec"`
	AudioTracks *int             `json:"audio_tracks"`
	SyncMode    string           `json:"sync_mode"`
	HasSidecar  bool             `json:"has_sidecar"`
	Missing     bool             `json:"missing"`
	Session     *Session         `json:"session"`
	ProxyURL    string           `json:"proxy_url,omitempty"`
	Sprite      *Sprite          `json:"sprite"`
	Stages      map[string]Stage `json:"stages"`
}

const recordingColumns = `
	r.id, r.source, r.path, r.size, r.recorded_at, r.duration, r.fps, r.width, r.height, r.video_codec,
	r.audio_tracks, r.sync_mode, r.sidecar_path IS NOT NULL, r.missing_since IS NOT NULL,
	r.proxy_path IS NOT NULL, r.sprite,
	s.id IS NOT NULL, coalesce(s.project, ''), coalesce(s.session_key, ''), coalesce(s.engine, ''),
	coalesce(s.build, ''), coalesce(s.platform, ''), coalesce(s.tester, '')`

func scanRecording(row pgx.Row) (*Recording, error) {
	var (
		rec        Recording
		hasProxy   bool
		sprite     []byte
		hasSession bool
		sess       Session
	)
	err := row.Scan(&rec.ID, &rec.Source, &rec.Path, &rec.Size, &rec.RecordedAt, &rec.Duration, &rec.FPS,
		&rec.Width, &rec.Height, &rec.VideoCodec, &rec.AudioTracks, &rec.SyncMode,
		&rec.HasSidecar, &rec.Missing, &hasProxy, &sprite,
		&hasSession, &sess.Project, &sess.SessionID, &sess.Engine, &sess.Build, &sess.Platform, &sess.Tester)
	if err != nil {
		return nil, err
	}
	rec.Name = path.Base(rec.Path)
	rec.Library = rec.Source == config.LibraryName
	if hasSession {
		rec.Session = &sess
	}
	base := fmt.Sprintf("/api/v1/recordings/%d", rec.ID)
	if hasProxy {
		rec.ProxyURL = base + "/proxy"
	}
	if len(sprite) > 0 {
		var sp Sprite
		if err := json.Unmarshal(sprite, &sp); err == nil {
			sp.URL = base + "/sprites/{sheet}"
			rec.Sprite = &sp
		}
	}
	rec.Stages = map[string]Stage{}
	return &rec, nil
}

// loadRecordings fetches recordings by id, with their latest stage states.
func (s *Server) loadRecordings(ctx context.Context, ids []int64) (map[int64]*Recording, error) {
	out := map[int64]*Recording{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT `+recordingColumns+`
		FROM recordings r LEFT JOIN sessions s ON s.id = r.session_id
		WHERE r.id = ANY ($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		rec, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out[rec.ID] = rec
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.attachStages(ctx, out)
}

func (s *Server) attachStages(ctx context.Context, recs map[int64]*Recording) error {
	ids := make([]int64, 0, len(recs))
	for id := range recs {
		ids = append(ids, id)
	}
	rows, err := s.DB.Query(ctx, `
		SELECT DISTINCT ON (recording_id, stage)
		       recording_id, stage, status, version, model, attempts, coalesce(error, ''), finished_at
		FROM jobs WHERE recording_id = ANY ($1)
		ORDER BY recording_id, stage, created_at DESC, id DESC`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id    int64
			name  string
			stage Stage
		)
		if err := rows.Scan(&id, &name, &stage.Status, &stage.Version, &stage.Model,
			&stage.Attempts, &stage.Error, &stage.Finished); err != nil {
			return err
		}
		recs[id].Stages[name] = stage
	}
	return rows.Err()
}

func (s *Server) listRecordings(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	var (
		list  []*Recording
		total int64
	)
	byID := map[int64]*Recording{}
	rows, err := s.DB.Query(r.Context(), `SELECT `+recordingColumns+`, count(*) OVER ()
		FROM recordings r LEFT JOIN sessions s ON s.id = r.session_id
		WHERE r.missing_since IS NULL
		ORDER BY r.recorded_at DESC, r.id DESC
		LIMIT $1 OFFSET $2`, limit, max(offset, 0))
	if err != nil {
		failErr(w, r, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		rec, err := scanRecording(totalScanner{rows, &total})
		if err != nil {
			failErr(w, r, err)
			return
		}
		list = append(list, rec)
		byID[rec.ID] = rec
	}
	if err := rows.Err(); err != nil {
		failErr(w, r, err)
		return
	}
	if err := s.attachStages(r.Context(), byID); err != nil {
		failErr(w, r, err)
		return
	}
	if list == nil {
		list = []*Recording{}
	}
	writeJSON(w, map[string]any{"recordings": list, "total": total})
}

// totalScanner appends a trailing count(*) OVER () column to a recording scan.
type totalScanner struct {
	pgx.Row
	total *int64
}

func (t totalScanner) Scan(dest ...any) error { return t.Row.Scan(append(dest, t.total)...) }

func (s *Server) getRecording(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	recs, err := s.loadRecordings(r.Context(), []int64{id})
	if err != nil {
		failErr(w, r, err)
		return
	}
	rec, found := recs[id]
	if !found {
		fail(w, http.StatusNotFound, "recording not found")
		return
	}
	writeJSON(w, rec)
}

type Event struct {
	ID         int64           `json:"id"`
	Type       string          `json:"type"`
	T          float64         `json:"t"`
	Dur        *float64        `json:"dur,omitempty"`
	Kind       string          `json:"kind"`
	Label      string          `json:"label,omitempty"`
	Source     string          `json:"source,omitempty"`
	Tags       []string        `json:"tags"`
	Importance int             `json:"importance"`
	Data       json.RawMessage `json:"data"`
}

func (s *Server) recordingEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	rows, err := s.DB.Query(r.Context(), `
		SELECT e.id, e.type, e.t + r.sync_offset, e.dur, e.kind, e.label, e.source, e.tags,
		       e.importance, e.data
		FROM events e JOIN recordings r ON r.id = e.recording_id
		WHERE e.recording_id = $1 ORDER BY e.t, e.id`, id)
	if err != nil {
		failErr(w, r, err)
		return
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
		var e Event
		err := row.Scan(&e.ID, &e.Type, &e.T, &e.Dur, &e.Kind, &e.Label, &e.Source, &e.Tags,
			&e.Importance, &e.Data)
		return e, err
	})
	if err != nil {
		failErr(w, r, err)
		return
	}
	if events == nil {
		events = []Event{}
	}
	writeJSON(w, events)
}

type Line struct {
	Track int     `json:"track"`
	TIn   float64 `json:"t_in"`
	TOut  float64 `json:"t_out"`
	Text  string  `json:"text"`
}

func (s *Server) recordingTranscript(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	rows, err := s.DB.Query(r.Context(), `
		SELECT tr.track, tr.t_in + r.sync_offset, tr.t_out + r.sync_offset, tr.text
		FROM transcripts tr JOIN recordings r ON r.id = tr.recording_id
		WHERE tr.recording_id = $1 ORDER BY tr.t_in, tr.track`, id)
	if err != nil {
		failErr(w, r, err)
		return
	}
	lines, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Line])
	if err != nil {
		failErr(w, r, err)
		return
	}
	if lines == nil {
		lines = []Line{}
	}
	writeJSON(w, lines)
}

// recordingProxy streams the proxy; ServeFile answers range requests, which
// the player needs to seek.
func (s *Server) recordingProxy(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var rel *string
	if err := s.DB.QueryRow(r.Context(), `SELECT proxy_path FROM recordings WHERE id = $1`, id).
		Scan(&rel); err != nil {
		failErr(w, r, err)
		return
	}
	if rel == nil {
		fail(w, http.StatusNotFound, "proxy not ready")
		return
	}
	p, ok := s.dataPath(*rel)
	if !ok {
		fail(w, http.StatusNotFound, "proxy not found")
		return
	}
	http.ServeFile(w, r, p)
}

func (s *Server) recordingSprite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	sheet, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("sheet"), ".jpg"))
	if err != nil || sheet < 1 {
		fail(w, http.StatusBadRequest, "sheet must be a positive integer")
		return
	}
	p, ok := s.dataPath(fmt.Sprintf("sprites/%d/sheet_%03d.jpg", id, sheet))
	if !ok {
		fail(w, http.StatusNotFound, "sprite not found")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, p)
}

// dataPath resolves a path under the data dir, refusing anything outside it.
func (s *Server) dataPath(rel string) (string, bool) {
	p := filepath.Join(s.Cfg.Data, filepath.FromSlash(rel))
	if r, err := filepath.Rel(s.Cfg.Data, p); err != nil || strings.HasPrefix(r, "..") {
		return "", false
	}
	return p, true
}

func (s *Server) reprocess(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	stage := r.URL.Query().Get("stage")
	if stage == "" {
		fail(w, http.StatusBadRequest, "stage is required")
		return
	}
	n, err := queue.Retry(r.Context(), s.DB, id, stage)
	if err != nil {
		failErr(w, r, err)
		return
	}
	if n == 0 {
		fail(w, http.StatusConflict, "no finished or failed "+stage+" job to re-run")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type Job struct {
	ID          int64           `json:"id"`
	RecordingID int64           `json:"recording_id"`
	Recording   string          `json:"recording"`
	Stage       string          `json:"stage"`
	Version     int             `json:"version"`
	Model       string          `json:"model,omitempty"`
	Status      string          `json:"status"`
	Attempts    int             `json:"attempts"`
	Worker      *string         `json:"worker"`
	Error       *string         `json:"error"`
	Result      json.RawMessage `json:"result"`
	CreatedAt   time.Time       `json:"created_at"`
	StartedAt   *time.Time      `json:"started_at"`
	FinishedAt  *time.Time      `json:"finished_at"`
}

func (s *Server) jobs(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	rows, err := s.DB.Query(r.Context(), `
		SELECT j.id, j.recording_id, r.path, j.stage, j.version, j.model, j.status, j.attempts,
		       j.worker, j.error, j.result, j.created_at, j.started_at, j.finished_at
		FROM jobs j JOIN recordings r ON r.id = j.recording_id
		WHERE $1 = '' OR j.status = $1
		ORDER BY (j.status = 'running') DESC, j.created_at DESC, j.id DESC
		LIMIT 200`, status)
	if err != nil {
		failErr(w, r, err)
		return
	}
	jobs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Job])
	if err != nil {
		failErr(w, r, err)
		return
	}
	if jobs == nil {
		jobs = []Job{}
	}
	writeJSON(w, jobs)
}
