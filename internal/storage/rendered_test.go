package storage

import "testing"

func TestHideRenderedPages(t *testing.T) {
	in := []Object{
		{Key: "a.md"}, {Key: "a.html"}, {Key: "b.html"}, {Key: "dir/c.markdown"}, {Key: "dir/c.html"},
	}
	out := hideRenderedPages(in)
	got := map[string]bool{}
	for _, o := range out {
		got[o.Key] = true
	}
	if len(out) != 3 || !got["a.md"] || !got["b.html"] || !got["dir/c.markdown"] {
		t.Fatalf("unexpected result: %+v", out)
	}
}
