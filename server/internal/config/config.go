// Package config reads the server's settings from FOOTAGE_* environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string
	// Root is where recordings are mounted, read-only.
	Root string
	// Data holds proxies and thumbnails; the app writes only here.
	Data string
	// WebDir is the built UI. Empty serves the API only.
	WebDir string
	Addr   string

	ScanInterval time.Duration
	// StableFor is how long a file's size must stay unchanged before it's ingested.
	StableFor time.Duration
	// TranscribeModel is the speech model passed to workers; "off" disables the stage.
	TranscribeModel string
	// ExportTargets rewrite Root to the path an editing machine sees.
	ExportTargets []ExportTarget
}

type ExportTarget struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL:     os.Getenv("FOOTAGE_DATABASE_URL"),
		Root:            env("FOOTAGE_ROOT", "/footage"),
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
	if c.ScanInterval, err = duration("FOOTAGE_SCAN_INTERVAL", 30*time.Second); err != nil {
		errs = append(errs, err)
	}
	if c.StableFor, err = duration("FOOTAGE_STABLE_FOR", time.Minute); err != nil {
		errs = append(errs, err)
	}
	if c.ExportTargets, err = targets(os.Getenv("FOOTAGE_EXPORT_TARGETS")); err != nil {
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

// targets parses "Resolve (Mac)=/Volumes/footage;Premiere=\\nas\footage".
func targets(v string) ([]ExportTarget, error) {
	var out []ExportTarget
	for part := range strings.SplitSeq(v, ";") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		name, prefix, ok := strings.Cut(part, "=")
		if !ok || strings.TrimSpace(name) == "" || prefix == "" {
			return nil, fmt.Errorf("FOOTAGE_EXPORT_TARGETS: want name=prefix, got %q", part)
		}
		out = append(out, ExportTarget{Name: strings.TrimSpace(name), Prefix: prefix})
	}
	return out, nil
}
