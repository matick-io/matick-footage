package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// An empty select must still carry items and recordings; without them the
// select page crashed mapping over undefined.
func TestSelectDetailKeepsEmptyItems(t *testing.T) {
	sel := &Select{ID: 1, Name: "Untitled select", Items: []SelectItem{}, Recordings: map[int64]*Recording{}}
	b, err := json.Marshal(selectDetail{sel, sel.Items, sel.Recordings})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"items":[]`, `"recordings":{}`, `"name":"Untitled select"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
}
