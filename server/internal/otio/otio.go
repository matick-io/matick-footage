// Package otio writes a select as an OpenTimelineIO timeline: one video track
// of clips that point at the original files. Resolve imports .otio directly;
// other editors go through the OTIO adapters.
package otio

import (
	"encoding/json"
	"math"
	"net/url"
	"path"
	"regexp"
	"strings"
)

type Clip struct {
	Name string
	// Path is where the editing machine finds the original file.
	Path     string
	FPS      float64
	Duration float64 // of the whole file, seconds; 0 when unknown
	TIn      float64
	TOut     float64
	Comment  string
}

const defaultRate = 30

// Timeline renders the clips in order. Clip.1 and friends are the oldest schema
// versions, which every OTIO reader upgrades.
func Timeline(name string, clips []Clip) ([]byte, error) {
	children := make([]any, 0, len(clips))
	for _, c := range clips {
		rate := c.FPS
		if rate <= 0 {
			rate = defaultRate
		}
		ref := map[string]any{
			"OTIO_SCHEMA": "ExternalReference.1",
			"name":        "",
			"target_url":  FileURL(c.Path),
			"metadata":    map[string]any{},
		}
		if c.Duration > 0 {
			ref["available_range"] = timeRange(0, c.Duration, rate)
		} else {
			ref["available_range"] = nil
		}
		meta := map[string]any{}
		if c.Comment != "" {
			meta["matick-footage"] = map[string]any{"comment": c.Comment}
		}
		children = append(children, map[string]any{
			"OTIO_SCHEMA":     "Clip.1",
			"name":            c.Name,
			"source_range":    timeRange(c.TIn, c.TOut-c.TIn, rate),
			"media_reference": ref,
			"metadata":        meta,
			"effects":         []any{},
			"markers":         []any{},
		})
	}
	return json.MarshalIndent(map[string]any{
		"OTIO_SCHEMA":       "Timeline.1",
		"name":              name,
		"global_start_time": nil,
		"metadata":          map[string]any{},
		"tracks": map[string]any{
			"OTIO_SCHEMA":  "Stack.1",
			"name":         "tracks",
			"source_range": nil,
			"metadata":     map[string]any{},
			"effects":      []any{},
			"markers":      []any{},
			"children": []any{map[string]any{
				"OTIO_SCHEMA":  "Track.1",
				"name":         "V1",
				"kind":         "Video",
				"source_range": nil,
				"metadata":     map[string]any{},
				"effects":      []any{},
				"markers":      []any{},
				"children":     children,
			}},
		},
	}, "", "  ")
}

func timeRange(start, dur, rate float64) map[string]any {
	return map[string]any{
		"OTIO_SCHEMA": "TimeRange.1",
		"start_time":  rationalTime(start, rate),
		"duration":    rationalTime(dur, rate),
	}
}

// rationalTime snaps to whole frames, which editors expect.
func rationalTime(seconds, rate float64) map[string]any {
	return map[string]any{
		"OTIO_SCHEMA": "RationalTime.1",
		"rate":        rate,
		"value":       math.Round(seconds * rate),
	}
}

var drivePath = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// FileURL turns a POSIX, Windows drive or UNC path into a file:// URL.
func FileURL(p string) string {
	switch {
	case strings.HasPrefix(p, `\\`): // \\server\share\file
		host, rest, _ := strings.Cut(strings.TrimPrefix(p, `\\`), `\`)
		return (&url.URL{Scheme: "file", Host: host, Path: "/" + strings.ReplaceAll(rest, `\`, "/")}).String()
	case drivePath.MatchString(p):
		return (&url.URL{Scheme: "file", Path: "/" + strings.ReplaceAll(p, `\`, "/")}).String()
	default:
		return (&url.URL{Scheme: "file", Path: path.Clean("/" + p)}).String()
	}
}

// MapPath rewrites the container's footage root to an export target's prefix.
// prefix may use Windows separators, which then apply to the rest of the path.
func MapPath(root, rel, prefix string) string {
	if prefix == "" {
		return path.Join(root, rel)
	}
	if strings.Contains(prefix, `\`) {
		return strings.TrimRight(prefix, `\`) + `\` + strings.ReplaceAll(rel, "/", `\`)
	}
	return strings.TrimRight(prefix, "/") + "/" + rel
}
