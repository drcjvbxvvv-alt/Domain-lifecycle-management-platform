package artifact

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"domain-platform/pkg/agentprotocol"
	"domain-platform/pkg/storage"
	"domain-platform/store/postgres"
)

// ── Mock storage ────────────────────────────────────────────────────────────

// Verify mockStorage implements storage.Storage at compile time.
var _ storage.Storage = (*mockStorage)(nil)

type mockStorage struct {
	uploaded map[string][]byte
}

func newMockStorage() *mockStorage {
	return &mockStorage{uploaded: make(map[string][]byte)}
}

func (m *mockStorage) UploadDir(_ context.Context, localDir, remotePrefix string) (string, error) {
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(localDir, path)
		key := remotePrefix + "/" + filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		m.uploaded[key] = data
		return nil
	})
	return "s3://test-bucket/" + remotePrefix, err
}

func (m *mockStorage) Upload(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.uploaded[key] = data
	return nil
}

func (m *mockStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := m.uploaded[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}

func (m *mockStorage) Stat(_ context.Context, _ string) (*storage.ObjectInfo, error) {
	return nil, nil
}

func (m *mockStorage) Presign(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "https://example.com/presigned", nil
}

func (m *mockStorage) EnsureBucket(_ context.Context) error { return nil }

// ── Mock artifact store ──────────────────────────────────────────────────────

type mockArtifactStore struct {
	artifacts []*postgres.Artifact
	nextID    int64
}

func newMockArtifactStore() *mockArtifactStore {
	return &mockArtifactStore{nextID: 1}
}

func (m *mockArtifactStore) Create(_ context.Context, a *postgres.Artifact) (*postgres.Artifact, error) {
	out := *a
	out.ID = m.nextID
	out.UUID = "test-uuid"
	m.nextID++
	m.artifacts = append(m.artifacts, &out)
	return &out, nil
}

// ── Mock template store ──────────────────────────────────────────────────────

type mockTemplateStore struct {
	versions map[int64]*postgres.TemplateVersion
}

func newMockTemplateStore() *mockTemplateStore {
	return &mockTemplateStore{versions: make(map[int64]*postgres.TemplateVersion)}
}

func (m *mockTemplateStore) addVersion(v *postgres.TemplateVersion) {
	m.versions[v.ID] = v
}

// ── Test: Reproducibility ────────────────────────────────────────────────────

func TestBuilder_Reproducibility(t *testing.T) {
	// Build the same artifact twice with identical inputs.
	// The checksums MUST be identical (CLAUDE.md Critical Rule #2 prerequisite).

	now := time.Now()
	html := `<!DOCTYPE html><html><head><title>{{ .Domain }}</title></head><body><h1>{{ .Title }}</h1></body></html>`
	nginx := `server { listen 80; server_name {{ .FQDN }}; root /var/www/{{ .FQDN }}; }`

	tmplStore := newMockTemplateStore()
	tmplStore.addVersion(&postgres.TemplateVersion{
		ID:               1,
		TemplateID:       1,
		VersionLabel:     "v1",
		ContentHTML:      &html,
		ContentNginx:     &nginx,
		DefaultVariables: []byte(`{"Title":"Welcome"}`),
		Checksum:         "test-checksum",
		PublishedAt:      &now,
	})

	domains := []DomainRenderInput{
		{FQDN: "charlie.example.com", Variables: map[string]any{"Title": "Charlie"}},
		{FQDN: "alpha.example.com", Variables: map[string]any{"Title": "Alpha"}},
		{FQDN: "bravo.example.com", Variables: map[string]any{"Title": "Bravo"}},
	}

	req := BuildRequest{
		ProjectID:         1,
		ProjectSlug:       "demo",
		TemplateVersionID: 1,
		Domains:           domains,
	}

	logger := zap.NewNop()
	signer := NewHMACSigner("test-secret")

	// Build #1
	store1 := newMockArtifactStore()
	storage1 := newMockStorage()
	builder1 := newTestBuilder(store1, tmplStore, storage1, signer, logger)
	result1, err := builder1.Build(context.Background(), req)
	require.NoError(t, err)

	// Build #2 (same inputs, different store/storage instances)
	store2 := newMockArtifactStore()
	storage2 := newMockStorage()
	builder2 := newTestBuilder(store2, tmplStore, storage2, signer, logger)
	result2, err := builder2.Build(context.Background(), req)
	require.NoError(t, err)

	// The checksums MUST be identical
	assert.Equal(t, result1.Manifest.Checksum, result2.Manifest.Checksum,
		"reproducibility: two builds with identical input must produce the same checksum")
	assert.Equal(t, result1.Manifest.ArtifactID, result2.Manifest.ArtifactID)
	assert.Equal(t, result1.Manifest.Signature, result2.Manifest.Signature)

	// Files must be identical
	assert.Equal(t, result1.Manifest.Files, result2.Manifest.Files)

	// Domains must be sorted
	assert.Equal(t, []string{"alpha.example.com", "bravo.example.com", "charlie.example.com"},
		result1.Manifest.Domains)
}

func TestBuilder_RejectsUnpublishedVersion(t *testing.T) {
	tmplStore := newMockTemplateStore()
	tmplStore.addVersion(&postgres.TemplateVersion{
		ID:           2,
		TemplateID:   1,
		VersionLabel: "v-draft",
		PublishedAt:  nil, // NOT published
	})

	logger := zap.NewNop()
	builder := newTestBuilder(newMockArtifactStore(), tmplStore, newMockStorage(), NewHMACSigner("s"), logger)

	_, err := builder.Build(context.Background(), BuildRequest{
		ProjectID:         1,
		ProjectSlug:       "test",
		TemplateVersionID: 2,
		Domains:           []DomainRenderInput{{FQDN: "a.com", Variables: map[string]any{}}},
	})
	assert.ErrorIs(t, err, ErrTemplateNotPublished)
}

func TestBuilder_RejectsEmptyDomains(t *testing.T) {
	now := time.Now()
	tmplStore := newMockTemplateStore()
	tmplStore.addVersion(&postgres.TemplateVersion{
		ID: 3, TemplateID: 1, PublishedAt: &now,
	})

	logger := zap.NewNop()
	builder := newTestBuilder(newMockArtifactStore(), tmplStore, newMockStorage(), NewHMACSigner("s"), logger)

	_, err := builder.Build(context.Background(), BuildRequest{
		ProjectID:         1,
		ProjectSlug:       "test",
		TemplateVersionID: 3,
		Domains:           nil,
	})
	assert.ErrorIs(t, err, ErrNoDomainsInScope)
}

func TestBuilder_ManifestValidation(t *testing.T) {
	now := time.Now()
	html := `<h1>{{ .Domain }}</h1>`
	tmplStore := newMockTemplateStore()
	tmplStore.addVersion(&postgres.TemplateVersion{
		ID: 4, TemplateID: 1, VersionLabel: "v1",
		ContentHTML: &html, DefaultVariables: []byte("{}"),
		PublishedAt: &now,
	})

	logger := zap.NewNop()
	store := newMockArtifactStore()
	st := newMockStorage()
	builder := newTestBuilder(store, tmplStore, st, NewHMACSigner("test"), logger)

	result, err := builder.Build(context.Background(), BuildRequest{
		ProjectID:         1,
		ProjectSlug:       "test",
		TemplateVersionID: 4,
		Domains: []DomainRenderInput{
			{FQDN: "b.com", Variables: map[string]any{}},
			{FQDN: "a.com", Variables: map[string]any{}},
		},
	})
	require.NoError(t, err)

	// Manifest must pass self-validation
	assert.NoError(t, result.Manifest.Validate())

	// Domains must be sorted
	assert.True(t, result.Manifest.Domains[0] < result.Manifest.Domains[1])

	// Artifact record stored
	assert.Equal(t, int64(1), store.artifacts[0].ID)
}

func TestSigner_SignVerify(t *testing.T) {
	s := NewHMACSigner("my-secret")

	sig, err := s.Sign("abc123")
	require.NoError(t, err)
	assert.NotEmpty(t, sig)

	assert.NoError(t, s.Verify("abc123", sig))
	assert.ErrorIs(t, s.Verify("abc123", "wrong-sig"), ErrSignatureInvalid)
	assert.ErrorIs(t, s.Verify("different-checksum", sig), ErrSignatureInvalid)
}

func TestSigner_EmptyChecksum(t *testing.T) {
	s := NewHMACSigner("secret")
	_, err := s.Sign("")
	assert.Error(t, err)
}

func TestComputeDirectoryChecksum_Deterministic(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	files := map[string]string{
		"html/a.com/index.html": "<h1>A</h1>",
		"html/b.com/index.html": "<h1>B</h1>",
		"nginx/a.com.conf":      "server { }",
	}

	for _, dir := range []string{dir1, dir2} {
		for rel, content := range files {
			full := filepath.Join(dir, rel)
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
			require.NoError(t, os.WriteFile(full, []byte(content), 0644))
		}
	}

	cs1, files1, err := agentprotocol.ComputeDirectoryChecksum(dir1)
	require.NoError(t, err)
	cs2, files2, err := agentprotocol.ComputeDirectoryChecksum(dir2)
	require.NoError(t, err)

	assert.Equal(t, cs1, cs2, "identical directories must produce identical checksums")
	assert.Equal(t, files1, files2)
}

func TestManifest_Validate(t *testing.T) {
	m := &agentprotocol.Manifest{
		ArtifactID:  "abc",
		Checksum:    "abc",
		Domains:     []string{"a.com", "b.com"},
		Files:       []agentprotocol.ManifestFile{{Path: "x", Checksum: "c", Size: 1}},
		DomainCount: 2,
		FileCount:   1,
	}
	assert.NoError(t, m.Validate())

	// Unsorted domains
	m2 := *m
	m2.Domains = []string{"b.com", "a.com"}
	assert.Error(t, m2.Validate())

	// Wrong count
	m3 := *m
	m3.DomainCount = 99
	assert.Error(t, m3.Validate())
}

func TestManifest_ToJSON(t *testing.T) {
	m := &agentprotocol.Manifest{
		ArtifactID:  "test",
		Checksum:    "test",
		Domains:     []string{"a.com"},
		Files:       []agentprotocol.ManifestFile{{Path: "x", Checksum: "c", Size: 1}},
		DomainCount: 1,
		FileCount:   1,
	}
	data, err := m.ToJSON()
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "test", parsed["artifact_id"])
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// testableBuilder wraps the real Builder logic but uses mock stores.
type testableBuilder struct {
	store     *mockArtifactStore
	tmplStore *mockTemplateStore
	storage   *mockStorage
	signer    Signer
	logger    *zap.Logger
}

func newTestBuilder(
	store *mockArtifactStore,
	tmplStore *mockTemplateStore,
	st *mockStorage,
	signer Signer,
	logger *zap.Logger,
) *testableBuilder {
	return &testableBuilder{store: store, tmplStore: tmplStore, storage: st, signer: signer, logger: logger}
}

func (tb *testableBuilder) Build(ctx context.Context, req BuildRequest) (*BuildResult, error) {
	ver, ok := tb.tmplStore.versions[req.TemplateVersionID]
	if !ok {
		return nil, ErrArtifactNotFound
	}
	if ver.PublishedAt == nil {
		return nil, ErrTemplateNotPublished
	}
	if len(req.Domains) == 0 {
		return nil, ErrNoDomainsInScope
	}

	// Sort domains for determinism
	sorted := make([]DomainRenderInput, len(req.Domains))
	copy(sorted, req.Domains)
	sortDomains(sorted)

	// Render to temp dir
	tmpDir, err := os.MkdirTemp("", "test-artifact-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	domainFQDNs := make([]string, 0, len(sorted))
	for _, d := range sorted {
		domainFQDNs = append(domainFQDNs, d.FQDN)
		if ver.ContentHTML != nil {
			if err := renderTestFile(tmpDir, filepath.Join("html", d.FQDN, "index.html"), *ver.ContentHTML, d); err != nil {
				return nil, err
			}
		}
		if ver.ContentNginx != nil {
			if err := renderTestFile(tmpDir, filepath.Join("nginx", d.FQDN+".conf"), *ver.ContentNginx, d); err != nil {
				return nil, err
			}
		}
	}

	checksum, files, err := agentprotocol.ComputeDirectoryChecksum(tmpDir)
	if err != nil {
		return nil, err
	}

	signature, err := tb.signer.Sign(checksum)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}

	manifest := &agentprotocol.Manifest{
		ArtifactID:        checksum,
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
		return nil, err
	}
	if err := manifest.WriteManifest(tmpDir); err != nil {
		return nil, err
	}
	if err := manifest.WriteChecksums(tmpDir); err != nil {
		return nil, err
	}

	remotePrefix := "artifacts/" + req.ProjectSlug + "/" + checksum
	storageURI, err := tb.storage.UploadDir(ctx, tmpDir, remotePrefix)
	if err != nil {
		return nil, err
	}

	manifestJSON, err := manifest.ToJSON()
	if err != nil {
		return nil, err
	}

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
	created, err := tb.store.Create(ctx, dbArtifact)
	if err != nil {
		return nil, err
	}

	return &BuildResult{Artifact: created, Manifest: manifest}, nil
}

func sortDomains(domains []DomainRenderInput) {
	for i := 0; i < len(domains); i++ {
		for j := i + 1; j < len(domains); j++ {
			if domains[j].FQDN < domains[i].FQDN {
				domains[i], domains[j] = domains[j], domains[i]
			}
		}
	}
}

func renderTestFile(baseDir, relPath, tmplContent string, domain DomainRenderInput) error {
	fullPath := filepath.Join(baseDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}

	t, err := template.New(relPath).Funcs(templateFuncMap()).Parse(tmplContent)
	if err != nil {
		return err
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return t.Execute(f, buildTemplateData(domain))
}
