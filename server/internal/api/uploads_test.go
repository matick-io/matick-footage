package api

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"
)

func folderBucket(t *testing.T) (*blob.Bucket, string) {
	t.Helper()
	dir := t.TempDir()
	b, err := fileblob.OpenBucket(dir, &fileblob.Options{NoTempDir: true, Metadata: fileblob.MetadataDontWrite})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, dir
}

func TestWriteLeavesNothingWhenShort(t *testing.T) {
	b, dir := folderBucket(t)
	ctx := context.Background()
	err := write(ctx, b, "uploads/a.mp4", strings.NewReader("short"), 10, nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("write = %v, want unexpected EOF", err)
	}
	if left, _ := os.ReadDir(dir + "/uploads"); len(left) > 0 {
		t.Fatalf("left behind %v", left[0].Name())
	}
	if err := write(ctx, b, "uploads/a.mp4", strings.NewReader("exactly10!"), 10, nil); err != nil {
		t.Fatal(err)
	}
}

func TestFreeKey(t *testing.T) {
	b, _ := folderBucket(t)
	ctx := context.Background()
	for _, want := range []string{"u/s.mp4", "u/s (2).mp4", "u/s (3).mp4"} {
		got, err := freeKey(ctx, b, "u/s.mp4")
		if err != nil || got != want {
			t.Fatalf("freeKey = %q, %v; want %q", got, err, want)
		}
		if err := b.WriteAll(ctx, got, []byte("x"), nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{
		"session_0142.mp4": true, "Boss fight (2).mov": true, "": false, ".hidden.mp4": false,
		"a/b.mp4": false, `a\b.mp4`: false, "a\nb.mp4": false, "..": false,
	} {
		if got := validName(name); got != want {
			t.Errorf("validName(%q) = %v", name, got)
		}
	}
}

func TestSizeText(t *testing.T) {
	for n, want := range map[int64]string{50 << 30: "50 GiB", 256 << 20: "256 MiB", 3 << 10: "3 KiB", 1536: "1536 bytes"} {
		if got := sizeText(n); got != want {
			t.Errorf("sizeText(%d) = %q, want %q", n, got, want)
		}
	}
}
