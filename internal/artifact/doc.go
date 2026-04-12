// Package artifact handles immutable, content-addressed artifact storage.
// Artifacts are build outputs (nginx confs, HTML bundles) stored in MinIO/S3.
// Once signed (signed_at IS NOT NULL), an artifact record is IMMUTABLE at the store layer.
// See CLAUDE.md Critical Rule #2 and ARCHITECTURE.md §2.4.
package artifact
