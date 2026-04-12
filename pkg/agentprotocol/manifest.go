package agentprotocol

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Manifest is the wire-format metadata for an artifact.
// The Agent parses this JSON to verify integrity before applying files.
//
// Field names are stable — changing them is a breaking protocol change.
// Do NOT add fields that embed non-deterministic values (time.Now, uuid.New)
// into Content; timestamps belong ONLY in the manifest envelope.
type Manifest struct {
	// ArtifactID is the content-addressed identifier (sha256 of the tarball).
	ArtifactID string `json:"artifact_id"`

	// ProjectSlug identifies the project (human-readable, URL-safe).
	ProjectSlug string `json:"project_slug"`

	// TemplateVersionID pins this artifact to a specific template version.
	TemplateVersionID int64 `json:"template_version_id"`

	// Checksum is the SHA-256 hex digest of the artifact content directory
	// (computed over sorted file paths + file contents).
	Checksum string `json:"checksum"`

	// Signature is the HMAC-SHA256 (Phase 1) or cosign/GPG (future) signature
	// over the Checksum value.
	Signature string `json:"signature,omitempty"`

	// Domains lists every FQDN included in this artifact, sorted lexically.
	Domains []string `json:"domains"`

	// Files lists every file path relative to the artifact root, sorted lexically.
	Files []ManifestFile `json:"files"`

	// DomainCount is len(Domains), denormalized for quick display.
	DomainCount int `json:"domain_count"`

	// FileCount is len(Files), denormalized for quick display.
	FileCount int `json:"file_count"`

	// TotalSizeBytes is the sum of all file sizes.
	TotalSizeBytes int64 `json:"total_size_bytes"`

	// BuiltAt is when the artifact was built (envelope timestamp, NOT in content).
	BuiltAt time.Time `json:"built_at"`
}

// ManifestFile describes a single file in the artifact.
type ManifestFile struct {
	// Path is relative to the artifact root (e.g., "html/example.com/index.html").
	Path string `json:"path"`

	// Checksum is the SHA-256 hex digest of this file's content.
	Checksum string `json:"checksum"`

	// Size in bytes.
	Size int64 `json:"size"`
}

// Validate checks that the manifest is internally consistent.
func (m *Manifest) Validate() error {
	if m.ArtifactID == "" {
		return fmt.Errorf("manifest: artifact_id is empty")
	}
	if m.Checksum == "" {
		return fmt.Errorf("manifest: checksum is empty")
	}
	if len(m.Domains) == 0 {
		return fmt.Errorf("manifest: domains list is empty")
	}
	if len(m.Files) == 0 {
		return fmt.Errorf("manifest: files list is empty")
	}
	if m.DomainCount != len(m.Domains) {
		return fmt.Errorf("manifest: domain_count %d != len(domains) %d", m.DomainCount, len(m.Domains))
	}
	if m.FileCount != len(m.Files) {
		return fmt.Errorf("manifest: file_count %d != len(files) %d", m.FileCount, len(m.Files))
	}

	// Domains must be sorted
	if !sort.StringsAreSorted(m.Domains) {
		return fmt.Errorf("manifest: domains are not sorted")
	}

	// Files must be sorted by path
	for i := 1; i < len(m.Files); i++ {
		if m.Files[i].Path <= m.Files[i-1].Path {
			return fmt.Errorf("manifest: files not sorted at index %d", i)
		}
	}

	return nil
}

// ToJSON serializes the manifest to deterministic JSON (sorted keys via struct field order).
func (m *Manifest) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// WriteManifest writes the manifest JSON to the given directory as "manifest.json".
func (m *Manifest) WriteManifest(dir string) error {
	data, err := m.ToJSON()
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644)
}

// WriteChecksums writes a CHECKSUMS file (one line per file: "<sha256>  <path>")
// sorted by path, compatible with sha256sum --check.
func (m *Manifest) WriteChecksums(dir string) error {
	f, err := os.Create(filepath.Join(dir, "CHECKSUMS"))
	if err != nil {
		return fmt.Errorf("create CHECKSUMS: %w", err)
	}
	defer f.Close()

	// Files are already sorted by path (enforced by Validate)
	for _, file := range m.Files {
		if _, err := fmt.Fprintf(f, "%s  %s\n", file.Checksum, file.Path); err != nil {
			return fmt.Errorf("write checksum line: %w", err)
		}
	}
	return nil
}

// ComputeDirectoryChecksum walks a directory and computes a deterministic
// checksum over all files (sorted by relative path). This is used both by the
// builder (to set Manifest.Checksum) and by the agent (to verify after download).
//
// The algorithm: for each file in sorted-path order, feed "path\n" + file_content
// into a running SHA-256. The final hex digest is the directory checksum.
func ComputeDirectoryChecksum(dir string) (string, []ManifestFile, error) {
	var files []ManifestFile

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}
		// Normalize to forward slashes for cross-platform determinism
		rel = filepath.ToSlash(rel)

		// Skip manifest and checksums files themselves
		if rel == "manifest.json" || rel == "CHECKSUMS" {
			return nil
		}

		checksum, size, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}

		files = append(files, ManifestFile{
			Path:     rel,
			Checksum: checksum,
			Size:     size,
		})
		return nil
	})
	if err != nil {
		return "", nil, fmt.Errorf("walk dir: %w", err)
	}

	// Sort by path for determinism
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	// Compute aggregate checksum: for each file, hash "path\n" + content_hash
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Path + "\n"))
		h.Write([]byte(f.Checksum + "\n"))
	}

	return fmt.Sprintf("%x", h.Sum(nil)), files, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), n, nil
}
