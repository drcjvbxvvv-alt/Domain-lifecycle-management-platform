package bootstrap

import (
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// NewStorage creates a MinIO client for S3-compatible object storage.
// In development this connects to a local MinIO instance; in production
// this can point to AWS S3 by setting endpoint to "s3.amazonaws.com".
func NewStorage(cfg StorageConfig) (*minio.Client, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("new minio client: %w", err)
	}
	return client, nil
}
