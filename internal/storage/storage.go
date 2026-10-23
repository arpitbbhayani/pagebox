package storage

import (
	"context"
	"io"
)

type Storage interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, etag string) (newEtag string, err error)
	Get(ctx context.Context, key string, etag string) (io.ReadCloser, string, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context) ([]Object, error)
}

// Overwrite, passed as the etag to Put, writes unconditionally.
const Overwrite = "*"

type Object struct {
	Key  string
	ETag string
	Size int64
}

var ErrPreconditionFailed = struct{error}{nil}
var ErrNotFound = struct{error}{nil}
