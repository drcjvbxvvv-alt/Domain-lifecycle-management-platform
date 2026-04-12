package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Artifact maps to the artifacts table row.
type Artifact struct {
	ID                int64      `db:"id"`
	UUID              string     `db:"uuid"`
	ProjectID         int64      `db:"project_id"`
	ReleaseID         *int64     `db:"release_id"`
	TemplateVersionID int64      `db:"template_version_id"`
	ArtifactID        string     `db:"artifact_id"`
	StorageURI        string     `db:"storage_uri"`
	Manifest          []byte     `db:"manifest"` // raw JSONB
	Checksum          string     `db:"checksum"`
	Signature         *string    `db:"signature"`
	DomainCount       int        `db:"domain_count"`
	FileCount         int        `db:"file_count"`
	TotalSizeBytes    int64      `db:"total_size_bytes"`
	BuiltAt           time.Time  `db:"built_at"`
	BuiltBy           *int64     `db:"built_by"`
	SignedAt          *time.Time `db:"signed_at"`
}

var (
	ErrArtifactNotFound   = errors.New("artifact not found")
	ErrArtifactImmutable  = errors.New("artifact is signed and immutable")
	ErrArtifactIDConflict = errors.New("artifact_id already exists")
)

// ArtifactStore handles artifact persistence.
// CLAUDE.md Critical Rule #2: Once signed_at IS NOT NULL, the artifact is IMMUTABLE.
// The Update method enforces this at the store layer.
type ArtifactStore struct {
	db *sqlx.DB
}

func NewArtifactStore(db *sqlx.DB) *ArtifactStore {
	return &ArtifactStore{db: db}
}

const artifactColumns = `id, uuid, project_id, release_id, template_version_id, artifact_id,
	storage_uri, manifest, checksum, signature, domain_count, file_count,
	total_size_bytes, built_at, built_by, signed_at`

// Create inserts a new artifact row.
func (s *ArtifactStore) Create(ctx context.Context, a *Artifact) (*Artifact, error) {
	var out Artifact
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO artifacts
		   (project_id, release_id, template_version_id, artifact_id, storage_uri,
		    manifest, checksum, signature, domain_count, file_count, total_size_bytes,
		    built_at, built_by, signed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING `+artifactColumns,
		a.ProjectID, a.ReleaseID, a.TemplateVersionID, a.ArtifactID,
		a.StorageURI, a.Manifest, a.Checksum, a.Signature,
		a.DomainCount, a.FileCount, a.TotalSizeBytes,
		a.BuiltAt, a.BuiltBy, a.SignedAt,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create artifact: %w", err)
	}
	return &out, nil
}

// GetByID fetches an artifact by its database ID.
func (s *ArtifactStore) GetByID(ctx context.Context, id int64) (*Artifact, error) {
	var a Artifact
	err := s.db.GetContext(ctx, &a,
		`SELECT `+artifactColumns+` FROM artifacts WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrArtifactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get artifact by id: %w", err)
	}
	return &a, nil
}

// GetByArtifactID fetches an artifact by its content-addressed artifact_id (SHA-256).
func (s *ArtifactStore) GetByArtifactID(ctx context.Context, artifactID string) (*Artifact, error) {
	var a Artifact
	err := s.db.GetContext(ctx, &a,
		`SELECT `+artifactColumns+` FROM artifacts WHERE artifact_id = $1`, artifactID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrArtifactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get artifact by artifact_id: %w", err)
	}
	return &a, nil
}

// ListByRelease returns all artifacts for a given release, newest first.
func (s *ArtifactStore) ListByRelease(ctx context.Context, releaseID int64) ([]Artifact, error) {
	var items []Artifact
	err := s.db.SelectContext(ctx, &items,
		`SELECT `+artifactColumns+` FROM artifacts WHERE release_id = $1 ORDER BY built_at DESC`,
		releaseID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts by release: %w", err)
	}
	return items, nil
}

// ListByProject returns artifacts for a project with cursor pagination.
func (s *ArtifactStore) ListByProject(ctx context.Context, projectID int64, cursor int64, limit int) ([]Artifact, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var items []Artifact
	err := s.db.SelectContext(ctx, &items,
		`SELECT `+artifactColumns+` FROM artifacts
		 WHERE project_id = $1 AND id > $2
		 ORDER BY id ASC LIMIT $3`,
		projectID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list artifacts by project: %w", err)
	}
	return items, nil
}

// SetReleaseID links an artifact to a release. Only allowed before signing.
func (s *ArtifactStore) SetReleaseID(ctx context.Context, id int64, releaseID int64) error {
	// Immutability guard
	if err := s.checkNotSigned(ctx, id); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE artifacts SET release_id = $1 WHERE id = $2 AND signed_at IS NULL`,
		releaseID, id)
	if err != nil {
		return fmt.Errorf("set release_id on artifact %d: %w", id, err)
	}
	return nil
}

// MarkSigned sets signed_at and signature. This is a one-way operation.
// After this call, no further updates are permitted (Critical Rule #2).
func (s *ArtifactStore) MarkSigned(ctx context.Context, id int64, signature string) (*Artifact, error) {
	var out Artifact
	err := s.db.QueryRowxContext(ctx,
		`UPDATE artifacts SET signature = $1, signed_at = NOW()
		 WHERE id = $2 AND signed_at IS NULL
		 RETURNING `+artifactColumns,
		signature, id).StructScan(&out)
	if errors.Is(err, sql.ErrNoRows) {
		// Either not found or already signed
		existing, getErr := s.GetByID(ctx, id)
		if getErr != nil {
			return nil, ErrArtifactNotFound
		}
		if existing.SignedAt != nil {
			return nil, ErrArtifactImmutable
		}
		return nil, ErrArtifactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mark artifact signed: %w", err)
	}
	return &out, nil
}

// checkNotSigned returns ErrArtifactImmutable if the artifact is already signed.
func (s *ArtifactStore) checkNotSigned(ctx context.Context, id int64) error {
	var signedAt *time.Time
	err := s.db.GetContext(ctx, &signedAt,
		`SELECT signed_at FROM artifacts WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrArtifactNotFound
	}
	if err != nil {
		return fmt.Errorf("check artifact immutability: %w", err)
	}
	if signedAt != nil {
		return ErrArtifactImmutable
	}
	return nil
}
