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
	// CopyObject performs a server-side copy from srcKey to dstKey within the
	// same backend, avoiding the GET+PUT round-trip through the API server.
	// On R2/S3 this maps to CopyObject (or UploadPartCopy for objects >5GB).
	// Used by CopySession (SCRUM-346) for materials, slide PNGs, video files,
	// and file_artifacts where src and dst live in the same bucket. Callers
	// should fall back to Get+Put on error or when the backend cannot copy.
	CopyObject(ctx context.Context, srcKey, dstKey string) error
	// DeletePrefix removes all objects whose keys start with the given prefix (e.g. "sessions/{sessionID}/").
	// Returns the number of objects deleted. Used for session deletion.
	DeletePrefix(ctx context.Context, prefix string) (deleted int, err error)
}
