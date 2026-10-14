// Package storage opens the places recordings live. Each source is a Go CDK
// bucket: a file:// folder today, and S3 or another bucket once its driver is
// linked in, without changes elsewhere.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"

	"github.com/matick-io/matick-footage/server/internal/config"
)

type Source struct {
	Name string
	// Library marks the source the app owns, the only one it writes to.
	Library bool
	Bucket  *blob.Bucket
	// dir is the folder of a file:// source; url is the bucket URL of others.
	dir, url string
}

// Open opens one source. Folders are opened so that writes put their temporary
// file next to the final one, since a rename can't cross mounts, and leave no
// .attrs files beside recordings.
func Open(ctx context.Context, name, rawURL string, library bool) (*Source, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("source %s: %w", name, err)
	}
	s := &Source{Name: name, Library: library}
	if u.Scheme == "file" {
		s.dir = filepath.FromSlash(u.Path)
		s.Bucket, err = fileblob.OpenBucket(s.dir, &fileblob.Options{
			CreateDir: library, NoTempDir: true, Metadata: fileblob.MetadataDontWrite,
		})
	} else {
		s.url = rawURL
		s.Bucket, err = blob.OpenBucket(ctx, rawURL)
	}
	if err != nil {
		return nil, fmt.Errorf("source %s: %w", name, err)
	}
	return s, nil
}

// Root is where the source's paths are relative to: its folder, or its bucket URL.
func (s *Source) Root() string {
	if s.dir != "" {
		return filepath.ToSlash(s.dir)
	}
	return s.url
}

// URL says where a worker reads key: a file:// URL on the mount the core and
// workers share, or a signed link it can fetch without credentials.
func (s *Source) URL(ctx context.Context, key string) (string, error) {
	if s.dir != "" {
		p := filepath.ToSlash(filepath.Join(s.dir, filepath.FromSlash(key)))
		return (&url.URL{Scheme: "file", Path: p}).String(), nil
	}
	return s.Bucket.SignedURL(ctx, key, &blob.SignedURLOptions{Expiry: 12 * time.Hour})
}

// Free reports the bytes free under a folder source; ok is false for buckets.
func (s *Source) Free() (free uint64, ok bool) {
	if s.dir == "" {
		return 0, false
	}
	return freeBytes(s.dir)
}

// Walk calls fn for every file in the source, one folder at a time, skipping
// hidden files and folders (.zfs snapshots, .Trash and the like) without
// descending into them.
func (s *Source) Walk(ctx context.Context, fn func(*blob.ListObject) error) error {
	return s.walk(ctx, "", fn)
}

func (s *Source) walk(ctx context.Context, prefix string, fn func(*blob.ListObject) error) error {
	it := s.Bucket.List(&blob.ListOptions{Prefix: prefix, Delimiter: "/"})
	var dirs []string
	for {
		obj, err := it.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if strings.HasPrefix(path.Base(obj.Key), ".") {
			continue
		}
		if obj.IsDir {
			dirs = append(dirs, obj.Key)
			continue
		}
		if err := fn(obj); err != nil {
			return err
		}
	}
	for _, d := range dirs {
		if err := s.walk(ctx, d, fn); err != nil {
			return err
		}
	}
	return nil
}

// Set is every configured source: the watched ones, then the library.
type Set struct {
	All     []*Source
	Library *Source
}

func OpenAll(ctx context.Context, cfg config.Config) (*Set, error) {
	set := &Set{}
	for _, w := range cfg.Watch {
		s, err := Open(ctx, w.Name, w.URL, false)
		if err != nil {
			set.Close()
			return nil, err
		}
		set.All = append(set.All, s)
	}
	lib, err := Open(ctx, config.LibraryName, cfg.Library, true)
	if err != nil {
		set.Close()
		return nil, err
	}
	set.All = append(set.All, lib)
	set.Library = lib
	return set, nil
}

func (s *Set) Get(name string) (*Source, bool) {
	for _, src := range s.All {
		if src.Name == name {
			return src, true
		}
	}
	return nil, false
}

func (s *Set) Close() {
	for _, src := range s.All {
		_ = src.Bucket.Close()
	}
}
