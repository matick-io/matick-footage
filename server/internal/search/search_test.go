package search

import "testing"

func TestPrefixQuery(t *testing.T) {
	for in, want := range map[string]string{
		"":                "",
		"  ":              "",
		"Ward pha":        "ward:* & pha:*",
		"boss.phase 2!":   "boss:* & phase:* & 2:*",
		"it's 'quoted' &": "it:* & s:* & quoted:*",
	} {
		if got := prefixQuery(in); got != want {
			t.Errorf("prefixQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEventWindow(t *testing.T) {
	dur := 30.0
	cases := []struct {
		m       Match
		in, out float64
	}{
		{Match{Type: "event", T: 100}, 98, 106},
		{Match{Type: "marker", T: 100}, 90, 102},
		{Match{Type: "span", T: 100, Dur: &dur}, 98, 132},
	}
	for _, c := range cases {
		in, out := eventWindow(c.m)
		if in != c.in || out != c.out {
			t.Errorf("%s: got %v-%v, want %v-%v", c.m.Type, in, out, c.in, c.out)
		}
	}
}

func TestClamp(t *testing.T) {
	d := 10.0
	if in, out := clamp(-2, 12, &d); in != 0 || out != 10 {
		t.Fatalf("got %v-%v", in, out)
	}
	if in, out := clamp(5, 12, nil); in != 5 || out != 12 {
		t.Fatalf("unknown duration: got %v-%v", in, out)
	}
}

func TestMergeFoldsOverlappingShortClips(t *testing.T) {
	events := []Clip{
		{RecordingID: 1, TIn: 98, TOut: 106, Matches: []Match{{Type: "event", Importance: 3}}},
		{RecordingID: 1, TIn: 0, TOut: 300, Matches: []Match{{Type: "span"}}}, // a level: too long to fold
		{RecordingID: 2, TIn: 98, TOut: 106, Matches: []Match{{Type: "event"}}},
	}
	lines := []Clip{
		{RecordingID: 1, TIn: 100, TOut: 105, Matches: []Match{{Source: "transcript"}}},
	}
	got := merge(events, lines)
	if len(got) != 3 {
		t.Fatalf("want 3 clips, got %d: %+v", len(got), got)
	}
	top := got[0]
	if top.RecordingID != 1 || top.TIn != 98 || top.TOut != 106 || len(top.Matches) != 2 {
		t.Fatalf("top clip should be the event with the transcript folded in: %+v", top)
	}
}
