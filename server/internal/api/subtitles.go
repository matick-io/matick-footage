package api

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// recordingSubtitles serves one audio track's transcript as WebVTT, the format
// the HTML <track> element loads: /subtitles/0.vtt for the first track.
func (s *Server) recordingSubtitles(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	name, isVTT := strings.CutSuffix(r.PathValue("file"), ".vtt")
	track, err := strconv.Atoi(name)
	if !isVTT || err != nil || track < 0 {
		fail(w, http.StatusBadRequest, "want <audio track>.vtt, e.g. 0.vtt")
		return
	}
	var exists bool
	if err := s.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM recordings WHERE id = $1)`, id).
		Scan(&exists); err != nil {
		failErr(w, r, err)
		return
	}
	if !exists {
		fail(w, http.StatusNotFound, "recording not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `
		SELECT tr.track, tr.t_in + r.sync_offset, tr.t_out + r.sync_offset, tr.text
		FROM transcripts tr JOIN recordings r ON r.id = tr.recording_id
		WHERE tr.recording_id = $1 AND tr.track = $2 ORDER BY tr.t_in`, id, track)
	if err != nil {
		failErr(w, r, err)
		return
	}
	lines, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Line])
	if err != nil {
		failErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	_, _ = w.Write(webVTT(lines))
}

var vttEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// webVTT writes lines as cues. A cue can't hold a blank line or end before it
// starts, and times before zero (a negative sync offset) clamp to zero.
func webVTT(lines []Line) []byte {
	var b bytes.Buffer
	b.WriteString("WEBVTT\n")
	for _, l := range lines {
		text := strings.Join(strings.Fields(vttEscape.Replace(l.Text)), " ")
		start, end := max(l.TIn, 0), max(l.TOut, 0)
		if text == "" || end <= start {
			continue
		}
		fmt.Fprintf(&b, "\n%s --> %s\n%s\n", vttTime(start), vttTime(end), text)
	}
	return b.Bytes()
}

// vttTime writes seconds as hh:mm:ss.ttt.
func vttTime(sec float64) string {
	ms := int64(math.Round(sec * 1000))
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3_600_000, ms/60_000%60, ms/1000%60, ms%1000)
}
