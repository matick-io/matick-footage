package api

import "testing"

func TestWebVTT(t *testing.T) {
	got := string(webVTT([]Line{
		{TIn: -0.2, TOut: 1.5, Text: " Ready? "},
		{TIn: 3725.25, TOut: 3727, Text: "Boss <phase> 2 & up\n\nnow"},
		{TIn: 5, TOut: 5, Text: "empty span"},
		{TIn: 6, TOut: 7, Text: "   "},
	}))
	want := `WEBVTT

00:00:00.000 --> 00:00:01.500
Ready?

01:02:05.250 --> 01:02:07.000
Boss &lt;phase&gt; 2 &amp; up now
`
	if got != want {
		t.Errorf("webVTT =\n%s\nwant\n%s", got, want)
	}
}
