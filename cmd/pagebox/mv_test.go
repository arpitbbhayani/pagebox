package main

import (
	"testing"

	"github.com/arpitbhayani/pagebox/internal/index"
)

func mvEntries(keys ...string) []index.Entry {
	var out []index.Entry
	for _, k := range keys {
		out = append(out, index.Entry{ID: index.TrimDocExt(k), Key: k})
	}
	return out
}

func TestPlanMoves(t *testing.T) {
	entries := mvEntries("a.md", "notes/b.html", "notes/deep/c.md", "other.md")

	cases := []struct {
		name  string
		srcs  []string
		dst   string
		isDir bool
		want  map[string]string
		err   bool
	}{
		{"rename keeps ext", []string{"a"}, "z", false, map[string]string{"a.md": "z.md"}, false},
		{"rename explicit ext", []string{"a.md"}, "z.markdown", false, map[string]string{"a.md": "z.markdown"}, false},
		{"into folder", []string{"a", "other"}, "notes/", true, map[string]string{"a.md": "notes/a.md", "other.md": "notes/other.md"}, false},
		{"rename folder", []string{"notes"}, "docs", false, map[string]string{"notes/b.html": "docs/b.html", "notes/deep/c.md": "docs/deep/c.md"}, false},
		{"folder into folder", []string{"notes/deep"}, "other-dir/", true, map[string]string{"notes/deep/c.md": "other-dir/deep/c.md"}, false},
		{"conflict", []string{"a"}, "other", false, nil, true},
		{"multi to file", []string{"a", "other"}, "z", false, nil, true},
		{"missing", []string{"nope"}, "z", false, nil, true},
		{"folder into itself", []string{"notes"}, "notes/deep/", true, nil, true},
		{"system file", []string{"a"}, "index", false, nil, true},
	}
	for _, c := range cases {
		moves, err := planMoves(entries, c.srcs, c.dst, c.isDir)
		if (err != nil) != c.err {
			t.Fatalf("%s: err=%v", c.name, err)
		}
		if c.err {
			continue
		}
		if len(moves) != len(c.want) {
			t.Fatalf("%s: got %d moves, want %d", c.name, len(moves), len(c.want))
		}
		for _, m := range moves {
			if c.want[m.from.Key] != m.toKey {
				t.Errorf("%s: %s → %s, want %s", c.name, m.from.Key, m.toKey, c.want[m.from.Key])
			}
		}
	}
}

func TestPlanMovesRejectsFileFolderNameClash(t *testing.T) {
	entries := []index.Entry{
		{ID: "notes/a", Key: "notes/a.md"},
		{ID: "draft", Key: "draft.md"},
	}
	// "notes.html" would sit beside the folder "notes".
	if _, err := planMoves(entries, []string{"draft"}, "notes.html", false); err == nil {
		t.Fatal("expected a clash with folder notes")
	}
	// "notes/a.md/x.md" would put a folder where a document exists.
	if _, err := planMoves(entries, []string{"draft"}, "notes/a/x.md", false); err == nil {
		t.Fatal("expected a clash with document notes/a")
	}
}
