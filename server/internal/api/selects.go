package api

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/matick-io/matick-footage/server/internal/config"
	"github.com/matick-io/matick-footage/server/internal/otio"
)

type SelectItem struct {
	ID          int64   `json:"id"`
	RecordingID int64   `json:"recording_id"`
	TIn         float64 `json:"t_in"`
	TOut        float64 `json:"t_out"`
	Comment     string  `json:"comment"`
}

type Select struct {
	ID         int64                `json:"id"`
	Name       string               `json:"name"`
	Owner      string               `json:"owner"`
	Notes      string               `json:"notes"`
	ItemCount  int                  `json:"item_count"`
	Duration   float64              `json:"duration"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
	Items      []SelectItem         `json:"items,omitempty"`
	Recordings map[int64]*Recording `json:"recordings,omitempty"`
}

// selectDetail is one select. Unlike the list, it keeps items and recordings
// when they're empty: the web app maps over them.
type selectDetail struct {
	*Select
	Items      []SelectItem         `json:"items"`
	Recordings map[int64]*Recording `json:"recordings"`
}

const selectColumns = `
	s.id, s.name, s.owner, s.notes,
	(SELECT count(*) FROM select_items i WHERE i.select_id = s.id),
	(SELECT coalesce(sum(i.t_out - i.t_in), 0) FROM select_items i WHERE i.select_id = s.id),
	s.created_at, s.updated_at`

func scanSelect(row pgx.Row) (*Select, error) {
	var sel Select
	err := row.Scan(&sel.ID, &sel.Name, &sel.Owner, &sel.Notes, &sel.ItemCount, &sel.Duration,
		&sel.CreatedAt, &sel.UpdatedAt)
	return &sel, err
}

func (s *Server) listSelects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `SELECT `+selectColumns+` FROM selects s ORDER BY s.updated_at DESC`)
	if err != nil {
		failErr(w, r, err)
		return
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Select, error) { return scanSelect(row) })
	if err != nil {
		failErr(w, r, err)
		return
	}
	if list == nil {
		list = []*Select{}
	}
	writeJSON(w, list)
}

type selectInput struct {
	Name  *string `json:"name"`
	Owner *string `json:"owner"`
	Notes *string `json:"notes"`
}

func (s *Server) createSelect(w http.ResponseWriter, r *http.Request) {
	var in selectInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	var id int64
	err := s.DB.QueryRow(r.Context(), `
		INSERT INTO selects (name, owner, notes) VALUES ($1, coalesce($2, ''), coalesce($3, ''))
		RETURNING id`, strings.TrimSpace(*in.Name), in.Owner, in.Notes).Scan(&id)
	if err != nil {
		failErr(w, r, err)
		return
	}
	s.writeSelect(w, r, id, http.StatusCreated)
}

func (s *Server) getSelect(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r, "id"); ok {
		s.writeSelect(w, r, id, http.StatusOK)
	}
}

func (s *Server) writeSelect(w http.ResponseWriter, r *http.Request, id int64, status int) {
	sel, err := scanSelect(s.DB.QueryRow(r.Context(), `SELECT `+selectColumns+` FROM selects s WHERE s.id = $1`, id))
	if err != nil {
		failErr(w, r, err)
		return
	}
	rows, err := s.DB.Query(r.Context(), `
		SELECT id, recording_id, t_in, t_out, comment FROM select_items
		WHERE select_id = $1 ORDER BY position, id`, id)
	if err != nil {
		failErr(w, r, err)
		return
	}
	if sel.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[SelectItem]); err != nil {
		failErr(w, r, err)
		return
	}
	if sel.Items == nil {
		sel.Items = []SelectItem{}
	}
	ids := make([]int64, 0, len(sel.Items))
	for _, it := range sel.Items {
		ids = append(ids, it.RecordingID)
	}
	if sel.Recordings, err = s.loadRecordings(r.Context(), ids); err != nil {
		failErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, selectDetail{sel, sel.Items, sel.Recordings})
}

func (s *Server) updateSelect(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in selectInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		fail(w, http.StatusBadRequest, "name must not be empty")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `
		UPDATE selects SET name = coalesce(trim($2), name), owner = coalesce($3, owner),
		       notes = coalesce($4, notes), updated_at = now()
		WHERE id = $1`, id, in.Name, in.Owner, in.Notes)
	if err != nil {
		failErr(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, http.StatusNotFound, "select not found")
		return
	}
	s.writeSelect(w, r, id, http.StatusOK)
}

func (s *Server) deleteSelect(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if _, err := s.DB.Exec(r.Context(), `DELETE FROM selects WHERE id = $1`, id); err != nil {
		failErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type itemInput struct {
	// ID keeps an existing item's identity across a PUT; zero adds a new one.
	ID          int64   `json:"id"`
	RecordingID int64   `json:"recording_id"`
	TIn         float64 `json:"t_in"`
	TOut        float64 `json:"t_out"`
	Comment     string  `json:"comment"`
}

func (in itemInput) validate() error {
	if in.RecordingID <= 0 {
		return errors.New("recording_id is required")
	}
	if in.TIn < 0 || in.TOut <= in.TIn {
		return fmt.Errorf("need 0 <= t_in < t_out, got %v-%v", in.TIn, in.TOut)
	}
	return nil
}

// addSelectItem appends one clip, as "add to select" from search does.
func (s *Server) addSelectItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in itemInput
	if !readJSON(w, r, &in) {
		return
	}
	if err := in.validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	err := pgx.BeginFunc(r.Context(), s.DB, func(tx pgx.Tx) error {
		if err := touchSelect(r, tx, id); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO select_items (select_id, recording_id, t_in, t_out, comment, position)
			VALUES ($1, $2, $3, $4, $5,
			  (SELECT coalesce(max(position), -1) + 1 FROM select_items WHERE select_id = $1))`,
			id, in.RecordingID, in.TIn, in.TOut, in.Comment)
		return err
	})
	if err != nil {
		failErr(w, r, err)
		return
	}
	s.writeSelect(w, r, id, http.StatusOK)
}

// replaceSelectItems sets the whole list, in order: reorder, trim and remove
// are all this one call. Items sent with an id keep it.
func (s *Server) replaceSelectItems(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var items []itemInput
	if !readJSON(w, r, &items) {
		return
	}
	for i, it := range items {
		if err := it.validate(); err != nil {
			fail(w, http.StatusBadRequest, fmt.Sprintf("item %d: %v", i, err))
			return
		}
	}
	err := pgx.BeginFunc(r.Context(), s.DB, func(tx pgx.Tx) error {
		if err := touchSelect(r, tx, id); err != nil {
			return err
		}
		keep := []int64{}
		for _, it := range items {
			if it.ID > 0 {
				keep = append(keep, it.ID)
			}
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM select_items WHERE select_id = $1 AND NOT (id = ANY ($2))`,
			id, keep); err != nil {
			return err
		}
		for i, it := range items {
			if it.ID > 0 {
				tag, err := tx.Exec(r.Context(), `
					UPDATE select_items SET recording_id = $3, t_in = $4, t_out = $5, comment = $6, position = $7
					WHERE id = $1 AND select_id = $2`, it.ID, id, it.RecordingID, it.TIn, it.TOut, it.Comment, i)
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 0 {
					return pgx.ErrNoRows
				}
				continue
			}
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO select_items (select_id, recording_id, t_in, t_out, comment, position)
				VALUES ($1, $2, $3, $4, $5, $6)`, id, it.RecordingID, it.TIn, it.TOut, it.Comment, i); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		failErr(w, r, err)
		return
	}
	s.writeSelect(w, r, id, http.StatusOK)
}

func touchSelect(r *http.Request, tx pgx.Tx, id int64) error {
	var found int64
	return tx.QueryRow(r.Context(), `UPDATE selects SET updated_at = now() WHERE id = $1 RETURNING id`, id).Scan(&found)
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9 ._-]+`)

// exportSelect downloads the select as a timeline for one export target.
func (s *Server) exportSelect(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if f := r.URL.Query().Get("format"); f != "" && f != "otio" {
		fail(w, http.StatusBadRequest, "format must be otio")
		return
	}
	var target *config.ExportTarget
	if name := r.URL.Query().Get("target"); name != "" {
		for i, t := range s.Cfg.ExportTargets {
			if t.Name == name {
				target = &s.Cfg.ExportTargets[i]
			}
		}
		if target == nil {
			fail(w, http.StatusBadRequest, "unknown export target "+name)
			return
		}
	}
	var name string
	if err := s.DB.QueryRow(r.Context(), `SELECT name FROM selects WHERE id = $1`, id).Scan(&name); err != nil {
		failErr(w, r, err)
		return
	}
	rows, err := s.DB.Query(r.Context(), `
		SELECT r.source, r.path, coalesce(r.fps, 0), coalesce(r.duration, 0), i.t_in, i.t_out, i.comment
		FROM select_items i JOIN recordings r ON r.id = i.recording_id
		WHERE i.select_id = $1 ORDER BY i.position, i.id`, id)
	if err != nil {
		failErr(w, r, err)
		return
	}
	clips, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (otio.Clip, error) {
		var (
			c           otio.Clip
			source, rel string
		)
		err := row.Scan(&source, &rel, &c.FPS, &c.Duration, &c.TIn, &c.TOut, &c.Comment)
		c.Path = s.exportPath(target, source, rel)
		c.Name = fmt.Sprintf("%s @ %s", rel[strings.LastIndex(rel, "/")+1:], clock(c.TIn))
		return c, err
	})
	if err != nil {
		failErr(w, r, err)
		return
	}
	data, err := otio.Timeline(name, clips)
	if err != nil {
		failErr(w, r, err)
		return
	}
	file := strings.TrimSpace(unsafeFileChars.ReplaceAllString(name, "_"))
	if file == "" {
		file = "select"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.otio"`, file))
	_, _ = w.Write(data)
}

// exportPath is where an editing machine finds a recording: under the
// target's prefix for its source, or else the path the server sees.
func (s *Server) exportPath(t *config.ExportTarget, source, rel string) string {
	if t != nil {
		if prefix, ok := t.Prefixes[source]; ok {
			return otio.MapPath("", rel, prefix)
		}
	}
	root := ""
	if src, ok := s.Sources.Get(source); ok {
		root = src.Root()
	}
	return otio.MapPath(root, rel, "")
}

func clock(sec float64) string {
	d := time.Duration(sec * float64(time.Second)).Round(time.Second)
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}
