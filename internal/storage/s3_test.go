package storage

import (
	"context"
	"strings"
	"testing"
)

func TestLocalBackend(t *testing.T) {
	backend := &localBackend{dir: "/tmp/pagebox_test"}
	ctx := context.Background()

	// Put
	etag, err := backend.Put(ctx, "test.html", strings.NewReader("hello"), 5, "")
	if err != nil {
		t.Fatalf("expected nil error on put, got %v", err)
	}
	if etag != "local-etag" {
		t.Errorf("expected local-etag, got %s", etag)
	}

	// Get (mocked to always fail in local backend as per our stub)
	_, _, err = backend.Get(ctx, "test.html", "")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
