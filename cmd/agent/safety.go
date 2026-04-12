// Safety boundary for cmd/agent (CLAUDE.md Critical Rule #3).
//
// This file declares the ONLY allowed shell-out points. No user input may flow
// into os/exec.Command — all arguments are hard-coded constants.
//
// The four allowed shell-out points:
//   1. nginx -t           — test nginx configuration
//   2. nginx -s reload    — reload nginx after config swap
//   3. local verify HTTP  — configured HTTP HEAD check against localhost
//   4. systemd restart    — self-restart via systemctl (future, not in P1)
//
// CI gate: `make check-agent-safety` scans this directory for violations.
// Any new os/exec call requires explicit Opus review approval with a // safe: comment.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec" // safe: only used via the four hard-coded constants below
	"path/filepath"
	"strings"

	"domain-platform/pkg/agentprotocol"
)

// Hard-coded shell-out constants. These are the ONLY values that may be
// passed to os/exec.Command in the entire cmd/agent/ directory.
const (
	nginxBin = "/usr/sbin/nginx" // safe: hard-coded path, no user input
)

var (
	nginxTestArgs   = []string{"-t"}           // safe: hard-coded args
	nginxReloadArgs = []string{"-s", "reload"} // safe: hard-coded args
)

// runNginxTest runs `nginx -t` to validate configuration.
// Returns nil on success, error with nginx output on failure.
func runNginxTest() error { // safe: hard-coded command, no variable input
	cmd := exec.Command(nginxBin, nginxTestArgs...) // safe: hard-coded binary + args
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx -t failed: %s: %w", string(output), err)
	}
	return nil
}

// runNginxReload runs `nginx -s reload` to apply new configuration.
func runNginxReload() error { // safe: hard-coded command, no variable input
	cmd := exec.Command(nginxBin, nginxReloadArgs...) // safe: hard-coded binary + args
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx -s reload failed: %s: %w", string(output), err)
	}
	return nil
}

// verifyArtifactChecksum verifies that the downloaded artifact directory matches
// the manifest checksum. Returns nil if the checksum matches.
func verifyArtifactChecksum(artifactDir string, manifest *agentprotocol.Manifest) error {
	checksum, _, err := agentprotocol.ComputeDirectoryChecksum(artifactDir)
	if err != nil {
		return fmt.Errorf("compute dir checksum: %w", err)
	}
	if checksum != manifest.Checksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", manifest.Checksum, checksum)
	}
	return nil
}

// verifyArtifactSignature verifies the HMAC signature on the manifest checksum.
func verifyArtifactSignature(manifest *agentprotocol.Manifest, secret string) error {
	if secret == "" {
		// No signing key configured — skip verification in dev mode
		return nil
	}

	mac := computeHMACSHA256([]byte(secret), []byte(manifest.Checksum))
	if mac != manifest.Signature {
		return fmt.Errorf("signature mismatch: expected %s, got %s", manifest.Signature, mac)
	}
	return nil
}

// computeHMACSHA256 computes HMAC-SHA256 and returns hex string.
func computeHMACSHA256(key, data []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// snapshotPrevious copies current deploy files to .previous/{releaseID}/ for rollback.
// CLAUDE.md Critical Rule #6: Every artifact deploy must snapshot the previous state.
func snapshotPrevious(deployPath, releaseID string) error {
	prevDir := filepath.Join(deployPath, ".previous", releaseID)
	if err := os.MkdirAll(prevDir, 0755); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}

	return filepath.Walk(deployPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(deployPath, path)
		// Skip .previous directory itself
		if rel == ".previous" || strings.HasPrefix(rel, ".previous"+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(prevDir, rel), info.Mode())
		}
		return copyFile(path, filepath.Join(prevDir, rel))
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
