package config

import (
	"reflect"
	"testing"
)

func TestSources(t *testing.T) {
	got, err := sources("footage=file:///footage; cameras = file:///mnt/cams")
	want := []Source{{"footage", "file:///footage"}, {"cameras", "file:///mnt/cams"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %v, %v; want %v", got, err, want)
	}
	for _, bad := range []string{"footage", "Footage=file:///x", "library=file:///x", "a=file:///x;a=file:///y"} {
		if _, err := sources(bad); err == nil {
			t.Errorf("sources(%q) accepted", bad)
		}
	}
}

func TestTargets(t *testing.T) {
	known := map[string]bool{"footage": true, LibraryName: true}
	got, err := targets(`Resolve (Mac)=footage:/Volumes/footage|library:/Volumes/library;Premiere=footage:D:\footage`, known)
	want := []ExportTarget{
		{"Resolve (Mac)", map[string]string{"footage": "/Volumes/footage", "library": "/Volumes/library"}},
		{"Premiere", map[string]string{"footage": `D:\footage`}},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %v, %v; want %v", got, err, want)
	}
	for _, bad := range []string{"Resolve=/Volumes/footage", "Resolve=cams:/Volumes/cams", "=footage:/x"} {
		if _, err := targets(bad, known); err == nil {
			t.Errorf("targets(%q) accepted", bad)
		}
	}
}

func TestSize(t *testing.T) {
	for in, want := range map[string]int64{"": 7, "1024": 1024, "2k": 2048, "50G": 50 << 30, "1T": 1 << 40} {
		t.Setenv("SIZE", in)
		if got, err := size("SIZE", 7); err != nil || got != want {
			t.Errorf("size(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"-1", "0", "G", "5X", "1.5G", "99999999T"} {
		t.Setenv("SIZE", bad)
		if _, err := size("SIZE", 7); err == nil {
			t.Errorf("size(%q) accepted", bad)
		}
	}
}
