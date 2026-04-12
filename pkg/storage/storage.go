package storage

import (
	"context"
	"io"
	"time"
)

// ObjectInfo describes a stored object's metadata.
type ObjectInfo struct {
	Key          string
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
}

// Storage is the artifact storage interface.
// Implementations: MinIO (pkg/storage/minio.go), mock (tests).
type Storage interface {
	// UploadDir uploads all files under localDir to the bucket under remotePrefix.
	// Returns the base URI (e.g., "s3://bucket/prefix") on success.
	UploadDir(ctx context.Context, localDir, remotePrefix string) (uri string, err error)

	// Upload uploads a single object.
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error

	// Download returns a reader for the given key.
	Download(ctx context.Context, key string) (io.ReadCloser, error)

	// Stat returns metadata for the given key without downloading content.
	Stat(ctx context.Context, key string) (*ObjectInfo, error)

	// Presign returns a pre-signed GET URL valid for the given duration.
	Presign(ctx context.Context, key string, ttl time.Duration) (url string, err error)

	// EnsureBucket creates the bucket if it doesn't exist.
	EnsureBucket(ctx context.Context) error

	// ListObjects returns metadata for all objects whose keys begin with prefix.
	ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error)

	// GetObjectContent downloads an object and returns its full contents.
	// Use only for small files (template renders, diff preview).
	GetObjectContent(ctx context.Context, key string) ([]byte, error)
}
