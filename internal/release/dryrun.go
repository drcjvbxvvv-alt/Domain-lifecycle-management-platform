package release

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.uber.org/zap"

	"domain-platform/pkg/agentprotocol"
	"domain-platform/store/postgres"

	"github.com/pmezard/go-difflib/difflib"
)

// DryRunResult is the response for a release dry-run preview.
type DryRunResult struct {
	ReleaseID     string      `json:"release_id"`
	NewArtifactID string      `json:"new_artifact_id"`
	OldArtifactID *string     `json:"old_artifact_id,omitempty"`
	Summary       DiffSummary `json:"summary"`
	Files         []FileDiff  `json:"files"`
}

// DiffSummary holds aggregate change counts.
type DiffSummary struct {
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Modified  int `json:"modified"`
	Unchanged int `json:"unchanged"`
}

// FileDiff represents a single file's change in the dry-run preview.
type FileDiff struct {
	Path   string `json:"path"`
	Change string `json:"change"` // "added" | "removed" | "modified" | "unchanged"
	Diff   string `json:"diff,omitempty"`  // unified diff for text files
}

// DryRun compares the release's artifact against the previous succeeded release's artifact.
// The release must have artifact_id set (i.e., be in ready, executing, or later state).
func (s *Service) DryRun(ctx context.Context, releaseDBID int64) (*DryRunResult, error) {
	rel, err := s.releases.GetByID(ctx, releaseDBID)
	if err != nil {
		return nil, fmt.Errorf("get release: %w", err)
	}
	if rel.ArtifactID == nil {
		return nil, fmt.Errorf("release %d has no artifact yet (still in %s state)", releaseDBID, rel.Status)
	}

	newArt, err := s.artifacts.GetByID(ctx, *rel.ArtifactID)
	if err != nil {
		return nil, fmt.Errorf("get new artifact: %w", err)
	}

	result := &DryRunResult{
		ReleaseID:     rel.ReleaseID,
		NewArtifactID: newArt.ArtifactID,
	}

	// Parse new artifact manifest
	var newManifest agentprotocol.Manifest
	if err := json.Unmarshal(newArt.Manifest, &newManifest); err != nil {
		return nil, fmt.Errorf("parse new manifest: %w", err)
	}

	// Find the previous succeeded release's artifact for comparison
	prevArt, prevManifest, err := s.findPreviousArtifact(ctx, rel)
	if err != nil {
		// No previous release — everything is "added"
		s.logger.Info("dry-run: no previous artifact found, treating all files as added",
			zap.Int64("release_id", releaseDBID))
		for _, mf := range newManifest.Files {
			result.Files = append(result.Files, FileDiff{Path: mf.Path, Change: "added"})
			result.Summary.Added++
		}
		return result, nil
	}

	oldArtID := prevArt.ArtifactID
	result.OldArtifactID = &oldArtID

	// Build lookup maps: path → ManifestFile
	newByPath := make(map[string]agentprotocol.ManifestFile, len(newManifest.Files))
	for _, mf := range newManifest.Files {
		newByPath[mf.Path] = mf
	}
	oldByPath := make(map[string]agentprotocol.ManifestFile, len(prevManifest.Files))
	for _, mf := range prevManifest.Files {
		oldByPath[mf.Path] = mf
	}

	artifactPrefix := fmt.Sprintf("artifacts/%s/%s", newManifest.ProjectSlug, newArt.Checksum)
	prevArtifactPrefix := fmt.Sprintf("artifacts/%s/%s", prevManifest.ProjectSlug, prevArt.Checksum)

	// Files in new but not old → added
	for path, newMF := range newByPath {
		if _, exists := oldByPath[path]; !exists {
			fd := FileDiff{Path: path, Change: "added"}
			if isTextFile(path) {
				if content, err := s.storage.GetObjectContent(ctx, artifactPrefix+"/"+newMF.Path); err == nil {
					fd.Diff = diffLines("", string(content), path)
				}
			}
			result.Files = append(result.Files, fd)
			result.Summary.Added++
		}
	}

	// Files in old but not new → removed
	for path, oldMF := range oldByPath {
		if _, exists := newByPath[path]; !exists {
			fd := FileDiff{Path: path, Change: "removed"}
			if isTextFile(path) {
				if content, err := s.storage.GetObjectContent(ctx, prevArtifactPrefix+"/"+oldMF.Path); err == nil {
					fd.Diff = diffLines(string(content), "", path)
				}
			}
			result.Files = append(result.Files, fd)
			result.Summary.Removed++
		}
	}

	// Files in both → check checksum
	for path, newMF := range newByPath {
		oldMF, exists := oldByPath[path]
		if !exists {
			continue // already handled above
		}
		if newMF.Checksum == oldMF.Checksum {
			result.Files = append(result.Files, FileDiff{Path: path, Change: "unchanged"})
			result.Summary.Unchanged++
		} else {
			fd := FileDiff{Path: path, Change: "modified"}
			if isTextFile(path) {
				oldContent, errOld := s.storage.GetObjectContent(ctx, prevArtifactPrefix+"/"+oldMF.Path)
				newContent, errNew := s.storage.GetObjectContent(ctx, artifactPrefix+"/"+newMF.Path)
				if errOld == nil && errNew == nil {
					fd.Diff = diffLines(string(oldContent), string(newContent), path)
				}
			}
			result.Files = append(result.Files, fd)
			result.Summary.Modified++
		}
	}

	return result, nil
}

// findPreviousArtifact returns the artifact from the last succeeded release in the same project.
func (s *Service) findPreviousArtifact(ctx context.Context, rel *postgres.Release) (*postgres.Artifact, *agentprotocol.Manifest, error) {
	prev, err := s.releases.GetLastSucceeded(ctx, rel.ProjectID, rel.ID)
	if err != nil {
		return nil, nil, err
	}
	if prev.ArtifactID == nil {
		return nil, nil, ErrNoPreviousRelease
	}
	art, err := s.artifacts.GetByID(ctx, *prev.ArtifactID)
	if err != nil {
		return nil, nil, fmt.Errorf("get prev artifact: %w", err)
	}
	var manifest agentprotocol.Manifest
	if err := json.Unmarshal(art.Manifest, &manifest); err != nil {
		return nil, nil, fmt.Errorf("parse prev manifest: %w", err)
	}
	return art, &manifest, nil
}

// diffLines returns a unified diff string between oldText and newText.
func diffLines(oldText, newText, filename string) string {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldText),
		B:        difflib.SplitLines(newText),
		FromFile: filename + " (old)",
		ToFile:   filename + " (new)",
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return ""
	}
	return text
}

// isTextFile returns true for file extensions that are safe to diff as text.
func isTextFile(path string) bool {
	textExts := []string{
		".html", ".htm", ".css", ".js", ".json", ".txt", ".xml",
		".conf", ".nginx", ".sh", ".yaml", ".yml", ".toml", ".md",
		".ts", ".tsx", ".jsx", ".svg",
	}
	lower := strings.ToLower(path)
	for _, ext := range textExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// isValidUTF8 checks if content is safe to diff as text.
func isValidUTF8(b []byte) bool {
	return utf8.Valid(b)
}
