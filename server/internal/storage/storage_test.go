package storage

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gocloud.dev/blob"
)

func TestWalkSkipsHidden(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{
		"a.mp4", "day/b.mp4", "day/b.events.jsonl", "day/deep/c.mov",
		".hidden.mp4", ".zfs/snapshot/a.mp4", "day/.Trash/d.mp4",
	} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src, err := Open(context.Background(), "footage", "file://"+filepath.ToSlash(dir), false)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Bucket.Close()
	var got []string
	err = src.Walk(context.Background(), func(o *blob.ListObject) error {
		got = append(got, o.Key)
		return nil
	})
	want := []string{"a.mp4", "day/b.events.jsonl", "day/b.mp4", "day/deep/c.mov"}
	slices.Sort(got)
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Walk = %v, %v; want %v", got, err, want)
	}
}

func TestFolderURL(t *testing.T) {
	src, err := Open(context.Background(), "library", "file://"+filepath.ToSlash(t.TempDir())+"/lib", true)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Bucket.Close()
	got, err := src.URL(context.Background(), "uploads/2026-10-14/session 1 (2).mp4")
	want := "file://" + src.Root() + "/uploads/2026-10-14/session%201%20%282%29.mp4"
	if err != nil || got != want {
		t.Fatalf("URL = %q, %v; want %q", got, err, want)
	}
	if _, ok := src.Free(); !ok {
		t.Error("Free not known for a folder")
	}
}
