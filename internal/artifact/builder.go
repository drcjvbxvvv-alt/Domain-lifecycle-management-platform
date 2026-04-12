package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/template"
	"time"

	"go.uber.org/zap"

	"domain-platform/pkg/agentprotocol"
	"domain-platform/pkg/storage"
	"domain-platform/store/postgres"
)

// BuildRequest contains all inputs needed to build an artifact.
// All fields that affect content MUST be deterministic — no time.Now(), no uuid.New().
type BuildRequest struct {
	ProjectID         int64
	ProjectSlug       string
	TemplateVersionID int64
	ReleaseID         *int64
	BuiltBy           *int64

	// Domains to render, with their variables. Sorted by FQDN by the caller.
	Domains []DomainRenderInput
}

// DomainRenderInput pairs a domain FQDN with its merged template variables.
type DomainRenderInput struct {
	FQDN      string
	Variables map[string]any
}

// BuildResult is returned by Builder.Build on success.
type BuildResult struct {
	Artifact *postgres.Artifact
	Manifest *agentprotocol.Manifest
}

// Builder renders templates for domains, uploads to storage, and signs.
type Builder struct {
	store   *postgres.ArtifactStore
	tmpl    *postgres.TemplateStore
	storage storage.Storage
	signer  Signer
	logger  *zap.Logger
}

// NewBuilder creates an artifact builder.
func NewBuilder(
	store *postgres.ArtifactStore,
	tmpl *postgres.TemplateStore,
	storage storage.Storage,
	signer Signer,
	logger *zap.Logger,
) *Builder {
	return &Builder{
		store:   store,
		tmpl:    tmpl,
		storage: storage,
		signer:  signer,
		logger:  logger,
	}
}

// Build renders all domain files from the template version, computes checksums,
// uploads to storage, signs, and persists the artifact record.
//
// Determinism contract:
//   - Domains are sorted by FQDN before rendering
//   - Variable maps are sorted by key during template execution (Go text/template handles this)
//   - No timestamps appear in rendered content (only in the manifest envelope)
//   - No random IDs in content
//   - Given identical BuildRequest inputs, the output checksum is identical
func (b *Builder) Build(ctx context.Context, req BuildRequest) (*BuildResult, error) {
	// 1. Fetch and validate the template version
	ver, err := b.tmpl.GetVersion(ctx, req.TemplateVersionID)
	if err != nil {
		return nil, fmt.Errorf("get template version %d: %w", req.TemplateVersionID, err)
	}
	if ver.PublishedAt == nil {
		return nil, ErrTemplateNotPublished
	}

	if len(req.Domains) == 0 {
		return nil, ErrNoDomainsInScope
	}

	// 2. Sort domains by FQDN for determinism
	sort.Slice(req.Domains, func(i, j int) bool {
		return req.Domains[i].FQDN < req.Domains[j].FQDN
	})

	// 3. Create temp directory for artifact content
	tmpDir, err := os.MkdirTemp("", "artifact-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 4. Render templates for each domain
	domainFQDNs := make([]string, 0, len(req.Domains))
	for _, d := range req.Domains {
		domainFQDNs = append(domainFQDNs, d.FQDN)

		if ver.ContentHTML != nil {
			if err := b.renderFile(tmpDir, filepath.Join("html", d.FQDN, "index.html"), *ver.ContentHTML, d); err != nil {
				return nil, fmt.Errorf("render html for %s: %w", d.FQDN, err)
			}
		}
		if ver.ContentNginx != nil {
			if err := b.renderFile(tmpDir, filepath.Join("nginx", d.FQDN+".conf"), *ver.ContentNginx, d); err != nil {
				return nil, fmt.Errorf("render nginx for %s: %w", d.FQDN, err)
			}
		}
	}

	// 5. Compute directory checksum (deterministic)
	checksum, files, err := agentprotocol.ComputeDirectoryChecksum(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("compute checksum: %w", err)
	}

	// 6. Sign the checksum
	signature, err := b.signer.Sign(checksum)
	if err != nil {
		return nil, fmt.Errorf("sign artifact: %w", err)
	}

	// 7. Build manifest (timestamps only here, not in content)
	now := time.Now().UTC()
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}

	manifest := &agentprotocol.Manifest{
		ArtifactID:        checksum, // content-addressed
		ProjectSlug:       req.ProjectSlug,
		TemplateVersionID: req.TemplateVersionID,
		Checksum:          checksum,
		Signature:         signature,
		Domains:           domainFQDNs,
		Files:             files,
		DomainCount:       len(domainFQDNs),
		FileCount:         len(files),
		TotalSizeBytes:    totalSize,
		BuiltAt:           now,
	}

	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("manifest validation: %w", err)
	}

	// 8. Write manifest + checksums into the artifact directory
	if err := manifest.WriteManifest(tmpDir); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}
	if err := manifest.WriteChecksums(tmpDir); err != nil {
		return nil, fmt.Errorf("write checksums: %w", err)
	}

	// 9. Upload to storage
	remotePrefix := fmt.Sprintf("artifacts/%s/%s", req.ProjectSlug, checksum)
	storageURI, err := b.storage.UploadDir(ctx, tmpDir, remotePrefix)
	if err != nil {
		return nil, fmt.Errorf("upload artifact: %w", err)
	}

	// 10. Marshal manifest for DB storage
	manifestJSON, err := manifest.ToJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal manifest json: %w", err)
	}

	// 11. Persist artifact record
	dbArtifact := &postgres.Artifact{
		ProjectID:         req.ProjectID,
		ReleaseID:         req.ReleaseID,
		TemplateVersionID: req.TemplateVersionID,
		ArtifactID:        checksum,
		StorageURI:        storageURI,
		Manifest:          manifestJSON,
		Checksum:          checksum,
		Signature:         &signature,
		DomainCount:       len(domainFQDNs),
		FileCount:         len(files),
		TotalSizeBytes:    totalSize,
		BuiltAt:           now,
		BuiltBy:           req.BuiltBy,
		SignedAt:           &now,
	}

	created, err := b.store.Create(ctx, dbArtifact)
	if err != nil {
		return nil, fmt.Errorf("persist artifact: %w", err)
	}

	b.logger.Info("artifact built",
		zap.String("artifact_id", checksum),
		zap.String("project", req.ProjectSlug),
		zap.Int("domain_count", len(domainFQDNs)),
		zap.Int("file_count", len(files)),
		zap.Int64("total_size_bytes", totalSize),
	)

	return &BuildResult{
		Artifact: created,
		Manifest: manifest,
	}, nil
}

// renderFile executes a Go text/template and writes the result to the artifact directory.
func (b *Builder) renderFile(baseDir, relPath, tmplContent string, domain DomainRenderInput) error {
	fullPath := filepath.Join(baseDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(fullPath), err)
	}

	// Parse template with deterministic function map
	t, err := template.New(relPath).Funcs(templateFuncMap()).Parse(tmplContent)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	// Build the template data — sorted keys for determinism (Go maps iterate
	// non-deterministically, but text/template accesses by key, not iteration,
	// so map order doesn't affect output for {{.VariableName}} access patterns).
	data := buildTemplateData(domain)

	if err := t.Execute(f, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	return nil
}

// buildTemplateData constructs the data map passed to text/template.Execute.
// Variables are merged: domain-specific variables override template defaults.
func buildTemplateData(domain DomainRenderInput) map[string]any {
	data := map[string]any{
		"FQDN":   domain.FQDN,
		"Domain": domain.FQDN,
	}
	// Merge domain variables (already provided pre-merged by the caller)
	for k, v := range domain.Variables {
		data[k] = v
	}
	return data
}

// templateFuncMap returns the function map available in artifact templates.
// Keep this minimal and deterministic — no time/random functions.
func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"toJSON": func(v any) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	}
}
