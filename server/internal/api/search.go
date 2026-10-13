package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/matick-io/matick-footage/server/internal/search"
)

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q, err := parseQuery(r.URL.Query())
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	clips, err := search.Run(r.Context(), s.DB, q)
	if err != nil {
		failErr(w, r, err)
		return
	}
	ids := make([]int64, 0, len(clips))
	for _, c := range clips {
		ids = append(ids, c.RecordingID)
	}
	recs, err := s.loadRecordings(r.Context(), ids)
	if err != nil {
		failErr(w, r, err)
		return
	}
	if clips == nil {
		clips = []search.Clip{}
	}
	writeJSON(w, map[string]any{"clips": clips, "recordings": recs})
}

func parseQuery(v url.Values) (search.Query, error) {
	q := search.Query{
		Text:      v.Get("q"),
		Kinds:     v["kind"],
		Tags:      v["tag"],
		Maps:      v["map"],
		Builds:    v["build"],
		Testers:   v["tester"],
		Platforms: v["platform"],
		Projects:  v["project"],
	}
	if s := v.Get("importance"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 || n > 3 {
			return q, errors.New("importance must be 0-3")
		}
		q.MinImportance = n
	}
	if s := v.Get("recording"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return q, errors.New("recording must be an id")
		}
		q.RecordingID = n
	}
	for key, dst := range map[string]**time.Time{"from": &q.From, "to": &q.To} {
		if s := v.Get(key); s != "" {
			t, err := parseDate(s)
			if err != nil {
				return q, errors.New(key + " must be a date (2006-01-02) or RFC 3339 time")
			}
			*dst = &t
		}
	}
	return q, nil
}

func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

type Facet struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// facets lists the values each filter can take, most common first.
func (s *Server) facets(w http.ResponseWriter, r *http.Request) {
	queries := map[string]string{
		"kinds": `SELECT e.kind, count(*) FROM events e JOIN recordings r ON r.id = e.recording_id
			WHERE r.missing_since IS NULL AND e.kind NOT IN ('level', 'marker') GROUP BY 1`,
		"tags": `SELECT tag, count(*) FROM events e JOIN recordings r ON r.id = e.recording_id,
			unnest(e.tags) tag WHERE r.missing_since IS NULL GROUP BY 1`,
		"maps": `SELECT e.label, count(DISTINCT e.recording_id) FROM events e
			JOIN recordings r ON r.id = e.recording_id
			WHERE r.missing_since IS NULL AND e.kind = 'level' AND e.label <> '' GROUP BY 1`,
	}
	for name, col := range map[string]string{
		"builds": "build", "testers": "tester", "platforms": "platform", "projects": "project",
	} {
		queries[name] = `SELECT s.` + col + `, count(*) FROM recordings r JOIN sessions s ON s.id = r.session_id
			WHERE r.missing_since IS NULL AND s.` + col + ` <> '' GROUP BY 1`
	}
	out := map[string][]Facet{}
	for name, q := range queries {
		rows, err := s.DB.Query(r.Context(), q+` ORDER BY 2 DESC, 1 LIMIT 200`)
		if err != nil {
			failErr(w, r, err)
			return
		}
		facets, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Facet])
		if err != nil {
			failErr(w, r, err)
			return
		}
		if facets == nil {
			facets = []Facet{}
		}
		out[name] = facets
	}
	writeJSON(w, out)
}
