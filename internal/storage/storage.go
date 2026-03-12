package storage

import (
	"context"
	"io"
	"time"
)

// Interface for R2 (or other backends) used for presigned PUT/GET and server-side uploads.
type Interface interface {
	// PresignPut returns a URL and headers the client must send when PUTting the file (e.g. Content-Type).
	PresignPut(ctx context.Context, key string, ttl time.Duration, contentType string) (url string, headers map[string]string, err error)
	// PresignGet returns a URL for reading the object (e.g. for <video src> or download).
	PresignGet(ctx context.Context, key string, ttl time.Duration) (url string, err error)
	// Put uploads from reader (used for Zoom ingest server-side). contentLength is optional (<=0 = unknown; R2 requires it so may buffer). Returns etag and size.
	Put(ctx context.Context, key string, reader io.Reader, contentType string, contentLength int64) (etag string, size int64, err error)
	// Get returns a reader for the object body. Caller must close the returned io.ReadCloser.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Head returns whether the object exists and its size/content-type.
	Head(ctx context.Context, key string) (exists bool, size int64, contentType string, err error)
	// Delete removes the object.
	Delete(ctx context.Context, key string) error
	// DeletePrefix removes all objects whose keys start with the given prefix (e.g. "sessions/{sessionID}/").
	// Returns the number of objects deleted. Used for session deletion.
	DeletePrefix(ctx context.Context, prefix string) (deleted int, err error)
}
