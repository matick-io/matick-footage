package otio

import (
	"encoding/json"
	"testing"
)

func TestFileURL(t *testing.T) {
	for in, want := range map[string]string{
		"/Volumes/footage/a b.mp4":      "file:///Volumes/footage/a%20b.mp4",
		`\\nas\footage\day 1\x.mp4`:     "file://nas/footage/day%201/x.mp4",
		`F:\footage\x.mp4`:              "file:///F:/footage/x.mp4",
		"/footage/../footage/sub/x.mp4": "file:///footage/sub/x.mp4",
	} {
		if got := FileURL(in); got != want {
			t.Errorf("FileURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapPath(t *testing.T) {
	if got := MapPath("/footage", "day1/x.mp4", ""); got != "/footage/day1/x.mp4" {
		t.Errorf("no prefix: %q", got)
	}
	if got := MapPath("/footage", "day1/x.mp4", "/Volumes/footage/"); got != "/Volumes/footage/day1/x.mp4" {
		t.Errorf("posix: %q", got)
	}
	if got := MapPath("/footage", "day1/x.mp4", `\\nas\footage`); got != `\\nas\footage\day1\x.mp4` {
		t.Errorf("unc: %q", got)
	}
}

func TestTimelineSnapsToFrames(t *testing.T) {
	data, err := Timeline("cut", []Clip{{Name: "a", Path: "/f/a.mp4", FPS: 60, Duration: 100, TIn: 10.004, TOut: 12.5}})
	if err != nil {
		t.Fatal(err)
	}
	var tl struct {
		Tracks struct {
			Children []struct {
				Children []struct {
					SourceRange struct {
						StartTime struct{ Value, Rate float64 } `json:"start_time"`
						Duration  struct{ Value float64 }       `json:"duration"`
					} `json:"source_range"`
					MediaReference struct {
						TargetURL string `json:"target_url"`
					} `json:"media_reference"`
				} `json:"children"`
			} `json:"children"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(data, &tl); err != nil {
		t.Fatal(err)
	}
	c := tl.Tracks.Children[0].Children[0]
	if c.SourceRange.StartTime.Value != 600 || c.SourceRange.StartTime.Rate != 60 || c.SourceRange.Duration.Value != 150 {
		t.Fatalf("source range: %+v", c.SourceRange)
	}
	if c.MediaReference.TargetURL != "file:///f/a.mp4" {
		t.Fatalf("target: %s", c.MediaReference.TargetURL)
	}
}
