// Package sidecar reads <recording>.events.jsonl files: a session header line,
// then one event, span or marker per line. It applies the same rules as
// schema/events.schema.json; a test keeps the two in agreement.
package sidecar

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"time"
)

// SchemaMajor is the format id and major version this reader understands.
const SchemaMajor = "matick-footage/events@1"

// Suffix is appended to a recording's base name to find its sidecar.
const Suffix = ".events.jsonl"

var kindPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

type Session struct {
	SessionID string
	Engine    string
	Project   string
	Build     string
	Platform  string
	Tester    string
	File      string
	Sync      string
	StartedAt *time.Time
	// Raw is the header line as written, kept so new fields stay queryable.
	Raw json.RawMessage
}

type Entry struct {
	Type       string // event, span or marker
	T          float64
	Dur        *float64
	Kind       string
	Label      string
	Source     string
	Tags       []string
	Importance int
	Data       json.RawMessage
}

// Problem is a line that was skipped.
type Problem struct {
	Line int
	Msg  string
}

func (p Problem) String() string { return fmt.Sprintf("line %d: %s", p.Line, p.Msg) }

type File struct {
	Session  Session
	Entries  []Entry
	Problems []Problem
}

// Parse reads a whole sidecar. It fails only when the session header is
// unusable; bad entry lines are skipped and reported in Problems.
func Parse(r io.Reader) (*File, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	f := &File{}
	line, header := 0, false
	for sc.Scan() {
		line++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		if !header {
			s, err := parseSession(raw)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			f.Session, header = s, true
			continue
		}
		e, err := parseEntry(raw)
		if err != nil {
			f.Problems = append(f.Problems, Problem{Line: line, Msg: err.Error()})
			continue
		}
		if e != nil {
			f.Entries = append(f.Entries, *e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !header {
		return nil, errors.New("empty sidecar: no session line")
	}
	return f, nil
}

type wireSession struct {
	Type      string `json:"type"`
	Schema    string `json:"schema"`
	SessionID string `json:"session_id"`
	Engine    string `json:"engine"`
	Project   string `json:"project"`
	Build     string `json:"build"`
	Platform  string `json:"platform"`
	Tester    string `json:"tester"`
	Recording struct {
		File      string `json:"file"`
		Sync      string `json:"sync"`
		StartedAt string `json:"started_at"`
	} `json:"recording"`
}

func parseSession(raw []byte) (Session, error) {
	var w wireSession
	if err := json.Unmarshal(raw, &w); err != nil {
		return Session{}, fmt.Errorf("session line is not valid JSON: %w", err)
	}
	if w.Type != "session" {
		return Session{}, fmt.Errorf("first line must have type \"session\", got %q", w.Type)
	}
	if w.Schema != SchemaMajor && !strings.HasPrefix(w.Schema, SchemaMajor+".") {
		return Session{}, fmt.Errorf("unsupported schema %q, want %s", w.Schema, SchemaMajor)
	}
	if w.SessionID == "" {
		return Session{}, errors.New("session_id is required")
	}
	switch w.Recording.Sync {
	case "", "obs", "timecode", "wallclock":
	default:
		return Session{}, fmt.Errorf("unknown recording.sync %q", w.Recording.Sync)
	}
	s := Session{
		SessionID: w.SessionID,
		Engine:    w.Engine,
		Project:   w.Project,
		Build:     w.Build,
		Platform:  w.Platform,
		Tester:    w.Tester,
		File:      w.Recording.File,
		Sync:      w.Recording.Sync,
		Raw:       append(json.RawMessage(nil), raw...),
	}
	if w.Recording.StartedAt != "" {
		t, err := time.Parse(time.RFC3339Nano, w.Recording.StartedAt)
		if err != nil {
			return Session{}, fmt.Errorf("recording.started_at: %w", err)
		}
		s.StartedAt = &t
	}
	return s, nil
}

type wireEntry struct {
	Type       string          `json:"type"`
	T          *float64        `json:"t"`
	Dur        *float64        `json:"dur"`
	Kind       string          `json:"kind"`
	Label      string          `json:"label"`
	Source     string          `json:"source"`
	Tags       []string        `json:"tags"`
	Importance *int            `json:"importance"`
	Data       json.RawMessage `json:"data"`
}

// parseEntry returns nil, nil for an entry type this version doesn't know,
// which a later minor version may add.
func parseEntry(raw []byte) (*Entry, error) {
	var w wireEntry
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	switch w.Type {
	case "event", "span", "marker":
	case "session":
		return nil, errors.New("a second session line")
	case "":
		return nil, errors.New("type is required")
	default:
		return nil, nil
	}
	if w.T == nil {
		return nil, errors.New("t is required")
	}
	if !validTime(*w.T) {
		return nil, fmt.Errorf("t must be a finite number >= 0, got %v", *w.T)
	}
	e := &Entry{Type: w.Type, T: *w.T, Kind: w.Kind, Label: w.Label, Source: w.Source}
	switch w.Type {
	case "marker":
		if e.Kind == "" {
			e.Kind = "marker"
		}
	case "span":
		if w.Dur == nil {
			return nil, errors.New("span needs dur")
		}
		if !validTime(*w.Dur) {
			return nil, fmt.Errorf("dur must be a finite number >= 0, got %v", *w.Dur)
		}
		e.Dur = w.Dur
	}
	if e.Kind == "" {
		return nil, errors.New("kind is required")
	}
	if !kindPattern.MatchString(e.Kind) {
		return nil, fmt.Errorf("kind %q must be dotted words of letters, digits, _ or -", e.Kind)
	}
	for _, tag := range w.Tags {
		if tag == "" {
			return nil, errors.New("tags must not be empty strings")
		}
	}
	e.Tags = w.Tags
	if w.Importance != nil {
		if *w.Importance < 0 || *w.Importance > 3 {
			return nil, fmt.Errorf("importance must be 0-3, got %d", *w.Importance)
		}
		e.Importance = *w.Importance
	}
	if len(w.Data) > 0 {
		if w.Data[0] != '{' {
			return nil, errors.New("data must be an object")
		}
		e.Data = w.Data
	}
	return e, nil
}

func validTime(v float64) bool { return v >= 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }
