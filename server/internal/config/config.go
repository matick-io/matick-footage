// Package config reads the server's settings from FOOTAGE_* environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LibraryName is the library's source name, in recordings and export targets.
const LibraryName = "library"

type Config struct {
	DatabaseURL string
	// Watch are sources something else writes to, such as camera shares. The
	// app only reads them.
	Watch []Source
	// Library is the bucket URL of the source the app owns: uploads land here,
	// and only its recordings can be deleted.
	Library string
	// Data holds proxies and thumbnails.
	Data string
	// WebDir is the built UI. Empty serves the API only.
	WebDir string
	Addr   string

	ScanInterval time.Duration
	// StableFor is how long a file's size must stay unchanged before it's ingested.
	StableFor time.Duration
	// TranscribeModel is the speech model passed to workers; "off" disables the stage.
	TranscribeModel string
	// UploadMax is the largest file an upload may be, in bytes.
	UploadMax int64
	// ExportTargets rewrite each source to the path an editing machine sees.
	ExportTargets []ExportTarget
}

// Source is a named bucket URL, e.g. footage=file:///footage.
type Source struct {
	Name string
	URL  string
}

type ExportTarget struct {
	Name string
	// Prefixes maps a source name to where the editing machine sees that source.
	Prefixes map[string]string
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL:     os.Getenv("FOOTAGE_DATABASE_URL"),
		Library:         env("FOOTAGE_LIBRARY", "file:///library"),
		Data:            env("FOOTAGE_DATA", "/data"),
		WebDir:          env("FOOTAGE_WEB_DIR", ""),
		Addr:            env("FOOTAGE_ADDR", ":8080"),
		TranscribeModel: env("FOOTAGE_TRANSCRIBE_MODEL", "small"),
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("FOOTAGE_DATABASE_URL is required"))
	}
	var err error
	if c.Watch, err = sources(env("FOOTAGE_WATCH", "footage=file:///footage")); err != nil {
		errs = append(errs, err)
	}
	if c.ScanInterval, err = duration("FOOTAGE_SCAN_INTERVAL", 30*time.Second); err != nil {
		errs = append(errs, err)
	}
	if c.StableFor, err = duration("FOOTAGE_STABLE_FOR", time.Minute); err != nil {
		errs = append(errs, err)
	}
	if c.UploadMax, err = size("FOOTAGE_UPLOAD_MAX", 50<<30); err != nil {
		errs = append(errs, err)
	}
	names := map[string]bool{LibraryName: true}
	for _, s := range c.Watch {
		names[s.Name] = true
	}
	if c.ExportTargets, err = targets(os.Getenv("FOOTAGE_EXPORT_TARGETS"), names); err != nil {
		errs = append(errs, err)
	}
	return c, errors.Join(errs...)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

// size parses a byte count with an optional K, M, G or T suffix (powers of 1024).
func size(key string, fallback int64) (int64, error) {
	v := strings.ToUpper(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback, nil
	}
	shift := 0
	if i := strings.IndexAny(v, "KMGT"); i >= 0 && i == len(v)-1 {
		shift = 10 * (1 + strings.IndexByte("KMGT", v[i]))
		v = v[:i]
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 || n > (1<<62)>>shift {
		return 0, fmt.Errorf("%s: want a size such as 50G, got %q", key, os.Getenv(key))
	}
	return n << shift, nil
}

var sourceName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// sources parses "footage=file:///footage;cameras=file:///cameras".
func sources(v string) ([]Source, error) {
	var out []Source
	seen := map[string]bool{}
	for part := range strings.SplitSeq(v, ";") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		name, url, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		switch {
		case !ok || url == "":
			return nil, fmt.Errorf("FOOTAGE_WATCH: want name=url, got %q", part)
		case !sourceName.MatchString(name):
			return nil, fmt.Errorf("FOOTAGE_WATCH: %q: names are lowercase letters, digits, - and _", name)
		case name == LibraryName || seen[name]:
			return nil, fmt.Errorf("FOOTAGE_WATCH: the name %q is taken", name)
		}
		seen[name] = true
		out = append(out, Source{Name: name, URL: strings.TrimSpace(url)})
	}
	return out, nil
}

// targets parses "Resolve (Mac)=footage:/Volumes/footage|library:/Volumes/library;…",
// one prefix per source the editing machine can reach.
func targets(v string, sources map[string]bool) ([]ExportTarget, error) {
	var out []ExportTarget
	for part := range strings.SplitSeq(v, ";") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		name, rest, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || rest == "" {
			return nil, fmt.Errorf("FOOTAGE_EXPORT_TARGETS: want name=source:prefix|source:prefix, got %q", part)
		}
		t := ExportTarget{Name: name, Prefixes: map[string]string{}}
		for m := range strings.SplitSeq(rest, "|") {
			// Cut at the first colon, so Windows prefixes like D:\footage survive.
			src, prefix, ok := strings.Cut(m, ":")
			src = strings.TrimSpace(src)
			if !ok || prefix == "" {
				return nil, fmt.Errorf("FOOTAGE_EXPORT_TARGETS: %s: want source:prefix, got %q", name, m)
			}
			if !sources[src] {
				return nil, fmt.Errorf("FOOTAGE_EXPORT_TARGETS: %s: no source named %q", name, src)
			}
			t.Prefixes[src] = prefix
		}
		out = append(out, t)
	}
	return out, nil
}
