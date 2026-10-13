package sidecar

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaDir = "../../../schema"

func TestParseBasicExample(t *testing.T) {
	f := parseFile(t, filepath.Join(schemaDir, "examples", "basic.events.jsonl"))
	if len(f.Problems) != 0 {
		t.Fatalf("problems: %v", f.Problems)
	}
	if f.Session.SessionID != "0001" || f.Session.Sync != "obs" || f.Session.StartedAt == nil {
		t.Fatalf("session: %+v", f.Session)
	}
	if got := len(f.Entries); got != 5 {
		t.Fatalf("entries: got %d, want 5", got)
	}
	marker := f.Entries[4]
	if marker.Kind != "marker" || marker.Source != "tester" || marker.T != 58.5 {
		t.Fatalf("marker: %+v", marker)
	}
	boss := f.Entries[3]
	if boss.Importance != 3 || string(boss.Data) != `{"boss":"Warden","phase":2}` {
		t.Fatalf("boss: %+v", boss)
	}
}

func TestParseSkipsBadLines(t *testing.T) {
	f := parseFile(t, filepath.Join(schemaDir, "examples", "invalid", "bad-lines.events.jsonl"))
	if len(f.Entries) != 0 {
		t.Fatalf("want every entry rejected, kept %+v", f.Entries)
	}
	if len(f.Problems) != 5 {
		t.Fatalf("want 5 problems, got %v", f.Problems)
	}
}

func TestParseRejectsBadHeader(t *testing.T) {
	for name, in := range map[string]string{
		"empty":         "",
		"not session":   `{"type":"event","t":1,"kind":"x"}`,
		"other major":   `{"type":"session","schema":"matick-footage/events@2","session_id":"1"}`,
		"no session id": `{"type":"session","schema":"matick-footage/events@1"}`,
	} {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestParseIgnoresUnknownTypesAndFields(t *testing.T) {
	in := `{"type":"session","schema":"matick-footage/events@1.3","session_id":"1","new_field":true}
{"type":"camera","t":1}
{"type":"event","t":1,"kind":"a.b","extra":"ok"}`
	f, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 1 || len(f.Problems) != 0 {
		t.Fatalf("entries %v problems %v", f.Entries, f.Problems)
	}
}

// TestSchemaAgreesWithParser checks every example line against the published
// JSON Schema and the parser, so the two can't drift apart.
func TestSchemaAgreesWithParser(t *testing.T) {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	sch, err := c.Compile(filepath.Join(schemaDir, "events.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(schemaDir, "examples", "*", "*.jsonl"))
	top, _ := filepath.Glob(filepath.Join(schemaDir, "examples", "*.jsonl"))
	files = append(files, top...)
	if len(files) == 0 {
		t.Fatal("no examples found")
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		for n := 1; sc.Scan(); n++ {
			line := sc.Bytes()
			var v any
			schemaOK := json.Unmarshal(line, &v) == nil && sch.Validate(v) == nil
			var parserOK bool
			if n == 1 {
				_, err := parseSession(line)
				parserOK = err == nil
			} else {
				e, err := parseEntry(line)
				parserOK = err == nil && e != nil
			}
			if schemaOK != parserOK {
				t.Errorf("%s:%d: schema valid=%v, parser valid=%v", filepath.Base(path), n, schemaOK, parserOK)
			}
		}
	}
}

func parseFile(t *testing.T, path string) *File {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	f, err := Parse(fh)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
