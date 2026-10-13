// Package search turns a query and filters into ranked clips. A clip is a
// recording plus in/out times, which is what every consumer works with.
package search

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Clip windows around a match, in seconds.
const (
	PreRoll  = 2.0
	PostRoll = 6.0
	// MarkerLookback: a tester presses "clip that" after the moment happened.
	MarkerLookback = 10.0
	// mergeMax is the longest clip that merges with overlapping ones.
	mergeMax = 30.0
	rrfK     = 60.0
	limit    = 200
)

type Query struct {
	Text          string
	Kinds         []string // prefix match: "boss" matches "boss.phase"
	Tags          []string
	Maps          []string // level span labels the clip falls inside
	Builds        []string
	Testers       []string
	Platforms     []string
	Projects      []string
	MinImportance int
	From, To      *time.Time
	RecordingID   int64
}

type Match struct {
	Source     string   `json:"source"` // event or transcript
	Type       string   `json:"type,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Label      string   `json:"label,omitempty"`
	Text       string   `json:"text,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Importance int      `json:"importance,omitempty"`
	T          float64  `json:"t"`
	Dur        *float64 `json:"dur,omitempty"`
}

type Clip struct {
	RecordingID int64   `json:"recording_id"`
	TIn         float64 `json:"t_in"`
	TOut        float64 `json:"t_out"`
	Score       float64 `json:"score"`
	Matches     []Match `json:"matches"`
}

func Run(ctx context.Context, db *pgxpool.Pool, q Query) ([]Clip, error) {
	tsq := prefixQuery(q.Text)
	events, err := searchEvents(ctx, db, q, tsq)
	if err != nil {
		return nil, fmt.Errorf("events: %w", err)
	}
	var lines []Clip
	// Transcripts carry no kinds, tags or importance, so those filters exclude them.
	if tsq != "" && len(q.Kinds) == 0 && len(q.Tags) == 0 && q.MinImportance == 0 {
		if lines, err = searchTranscripts(ctx, db, q, tsq); err != nil {
			return nil, fmt.Errorf("transcripts: %w", err)
		}
	}
	return merge(events, lines), nil
}

// prefixQuery makes "ward pha" match "Warden phase": every word is a prefix and
// all must match. Empty when there's nothing to search for.
func prefixQuery(text string) string {
	var terms []string
	for _, w := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		terms = append(terms, strings.ToLower(w)+":*")
	}
	return strings.Join(terms, " & ")
}

// filters builds the WHERE clauses shared by both lists. tAlias is the
// expression for the match time, used by the map filter.
type filters struct {
	where []string
	args  []any
}

func (f *filters) add(clause string, arg any) {
	f.args = append(f.args, arg)
	f.where = append(f.where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(f.args))))
}

func (f *filters) sql() string {
	if len(f.where) == 0 {
		return "TRUE"
	}
	return strings.Join(f.where, " AND ")
}

func common(q Query, tExpr string) *filters {
	f := &filters{where: []string{"r.missing_since IS NULL"}}
	if q.RecordingID != 0 {
		f.add("r.id = ?", q.RecordingID)
	}
	if len(q.Builds) > 0 {
		f.add("s.build = ANY (?)", q.Builds)
	}
	if len(q.Testers) > 0 {
		f.add("s.tester = ANY (?)", q.Testers)
	}
	if len(q.Platforms) > 0 {
		f.add("s.platform = ANY (?)", q.Platforms)
	}
	if len(q.Projects) > 0 {
		f.add("s.project = ANY (?)", q.Projects)
	}
	if q.From != nil {
		f.add("r.recorded_at >= ?", *q.From)
	}
	if q.To != nil {
		f.add("r.recorded_at < ?", *q.To)
	}
	if len(q.Maps) > 0 {
		f.add(`EXISTS (SELECT 1 FROM events l
			WHERE l.recording_id = r.id AND l.kind = 'level' AND l.label = ANY (?)
			  AND `+tExpr+` >= l.t AND `+tExpr+` < l.t + coalesce(l.dur, 'Infinity'))`, q.Maps)
	}
	return f
}

func searchEvents(ctx context.Context, db *pgxpool.Pool, q Query, tsq string) ([]Clip, error) {
	f := common(q, "e.t")
	rank, order := "0::real", "e.importance DESC, r.recorded_at DESC, e.t"
	if tsq != "" {
		f.add("e.search @@ to_tsquery('simple', ?)", tsq)
		rank = fmt.Sprintf("ts_rank(e.search, to_tsquery('simple', $%d))", len(f.args))
		order = "rank DESC, e.importance DESC, r.recorded_at DESC"
	}
	if len(q.Kinds) > 0 {
		var ors []string
		for _, k := range q.Kinds {
			f.args = append(f.args, k, k+".%")
			ors = append(ors, fmt.Sprintf("e.kind = $%d OR e.kind LIKE $%d", len(f.args)-1, len(f.args)))
		}
		f.where = append(f.where, "("+strings.Join(ors, " OR ")+")")
	}
	if len(q.Tags) > 0 {
		f.add("e.tags && ?", q.Tags)
	}
	if q.MinImportance > 0 {
		f.add("e.importance >= ?", q.MinImportance)
	}
	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT e.recording_id, e.type, e.t + r.sync_offset, e.dur, e.kind, e.label, e.tags,
		       e.importance, r.duration, %s AS rank
		FROM events e
		JOIN recordings r ON r.id = e.recording_id
		LEFT JOIN sessions s ON s.id = r.session_id
		WHERE %s
		ORDER BY %s
		LIMIT %d`, rank, f.sql(), order, limit), f.args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Clip, error) {
		var (
			c        Clip
			m        = Match{Source: "event"}
			duration *float64
			rank     float32
		)
		err := row.Scan(&c.RecordingID, &m.Type, &m.T, &m.Dur, &m.Kind, &m.Label, &m.Tags,
			&m.Importance, &duration, &rank)
		c.TIn, c.TOut = eventWindow(m)
		c.TIn, c.TOut = clamp(c.TIn, c.TOut, duration)
		c.Matches = []Match{m}
		return c, err
	})
}

func searchTranscripts(ctx context.Context, db *pgxpool.Pool, q Query, tsq string) ([]Clip, error) {
	f := common(q, "tr.t_in")
	f.add("tr.search @@ to_tsquery('english', ?)", tsq)
	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT tr.recording_id, tr.t_in + r.sync_offset, tr.t_out + r.sync_offset, tr.text, r.duration
		FROM transcripts tr
		JOIN recordings r ON r.id = tr.recording_id
		LEFT JOIN sessions s ON s.id = r.session_id
		WHERE %s
		ORDER BY ts_rank(tr.search, to_tsquery('english', $%d)) DESC, r.recorded_at DESC
		LIMIT %d`, f.sql(), len(f.args), limit), f.args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Clip, error) {
		var (
			c        Clip
			tOut     float64
			duration *float64
			m        = Match{Source: "transcript"}
		)
		err := row.Scan(&c.RecordingID, &m.T, &tOut, &m.Text, &duration)
		d := tOut - m.T
		m.Dur = &d
		c.TIn, c.TOut = clamp(m.T-PreRoll, tOut+PreRoll, duration)
		c.Matches = []Match{m}
		return c, err
	})
}

func eventWindow(m Match) (float64, float64) {
	switch {
	case m.Type == "marker":
		return m.T - MarkerLookback, m.T + PreRoll
	case m.Dur != nil:
		return m.T - PreRoll, m.T + *m.Dur + PreRoll
	default:
		return m.T - PreRoll, m.T + PostRoll
	}
}

func clamp(in, out float64, duration *float64) (float64, float64) {
	in = max(in, 0)
	if duration != nil && *duration > 0 {
		out = min(out, *duration)
	}
	return in, max(out, in)
}

// merge scores both ranked lists with reciprocal rank fusion, boosts important
// moments and tester markers, and folds short clips that overlap into one.
func merge(lists ...[]Clip) []Clip {
	var all []Clip
	for _, list := range lists {
		for i, c := range list {
			c.Score = 1 / (rrfK + float64(i+1))
			for _, m := range c.Matches {
				c.Score += 0.003 * float64(m.Importance)
				if m.Type == "marker" {
					c.Score += 0.005
				}
			}
			all = append(all, c)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })

	var out []Clip
	for _, c := range all {
		folded := false
		for i := range out {
			if o := &out[i]; mergeable(o, &c) {
				o.TIn, o.TOut = min(o.TIn, c.TIn), max(o.TOut, c.TOut)
				o.Score += c.Score / 2
				o.Matches = append(o.Matches, c.Matches...)
				folded = true
				break
			}
		}
		if !folded {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// mergeable: same recording, both short, and overlapping by at least half of
// the shorter one.
func mergeable(a, b *Clip) bool {
	if a.RecordingID != b.RecordingID {
		return false
	}
	la, lb := a.TOut-a.TIn, b.TOut-b.TIn
	if la > mergeMax || lb > mergeMax {
		return false
	}
	overlap := min(a.TOut, b.TOut) - max(a.TIn, b.TIn)
	return overlap > 0 && overlap >= min(la, lb)/2
}
