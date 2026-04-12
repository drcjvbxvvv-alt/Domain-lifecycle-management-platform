package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"domain-platform/pkg/agentprotocol"
)

// agentConfig holds runtime configuration for the agent's task handler.
type agentConfig struct {
	DeployPath   string // default: /var/www
	NginxPath    string // default: /etc/nginx/conf.d
	StagingPath  string // default: /tmp/agent-staging
	SigningKey   string // HMAC secret for signature verification
	AllowReload  bool   // whether nginx reload is permitted
}

// handleTask executes a task envelope through the deployment pipeline.
// The pipeline phases:
//   1. download     — fetch artifact files from S3
//   2. verify_checksum  — verify SHA-256 against manifest
//   3. verify_signature — verify HMAC signature
//   4. write        — write files to staging path
//   5. nginx_test   — run nginx -t (if applicable)
//   6. snapshot     — snapshot previous files for rollback
//   7. swap         — move staging → real path
//   8. reload       — nginx -s reload (if allowed)
//   9. local_verify — HTTP check against localhost
//
// Returns a TaskReport with the result.
func handleTask(ctx context.Context, client *http.Client, env *agentprotocol.TaskEnvelope, cfg agentConfig, logger *zap.Logger) *agentprotocol.TaskReport {
	start := time.Now()
	report := &agentprotocol.TaskReport{
		TaskID: env.TaskID,
		Status: agentprotocol.TaskStatusSucceeded,
	}

	var phases []agentprotocol.PhaseReport

	// Determine deploy/nginx paths from envelope or config
	deployPath := env.DeployPath
	if deployPath == "" {
		deployPath = cfg.DeployPath
	}
	nginxPath := env.NginxPath
	if nginxPath == "" {
		nginxPath = cfg.NginxPath
	}

	// Phase 1: Download artifact
	phase := runPhase("download", func() error {
		stagingDir := filepath.Join(cfg.StagingPath, env.TaskID)
		return downloadArtifact(ctx, client, env.ArtifactURL, stagingDir, &env.Manifest)
	})
	phases = append(phases, phase)
	if phase.Status == "failed" {
		return failReport(report, phases, start, phase.Detail)
	}

	stagingDir := filepath.Join(cfg.StagingPath, env.TaskID)
	defer os.RemoveAll(stagingDir)

	// Phase 2: Verify checksum
	phase = runPhase("verify_checksum", func() error {
		return verifyArtifactChecksum(stagingDir, &env.Manifest)
	})
	phases = append(phases, phase)
	if phase.Status == "failed" {
		return failReport(report, phases, start, phase.Detail)
	}

	// Phase 3: Verify signature
	phase = runPhase("verify_signature", func() error {
		return verifyArtifactSignature(&env.Manifest, cfg.SigningKey)
	})
	phases = append(phases, phase)
	if phase.Status == "failed" {
		return failReport(report, phases, start, phase.Detail)
	}

	// Phase 4: Write files
	phase = runPhase("write", func() error {
		return writeArtifactFiles(stagingDir, deployPath, nginxPath, env)
	})
	phases = append(phases, phase)
	if phase.Status == "failed" {
		return failReport(report, phases, start, phase.Detail)
	}

	// Phase 5: nginx -t (if this task has nginx config)
	hasNginx := env.Type == agentprotocol.TaskTypeDeployNginx || env.Type == agentprotocol.TaskTypeDeployFull
	if hasNginx {
		phase = runPhase("nginx_test", func() error {
			return runNginxTest()
		})
		phases = append(phases, phase)
		if phase.Status == "failed" {
			return failReport(report, phases, start, phase.Detail)
		}
	} else {
		phases = append(phases, agentprotocol.PhaseReport{Phase: "nginx_test", Status: "skipped"})
	}

	// Phase 6: Snapshot previous state
	phase = runPhase("snapshot", func() error {
		return snapshotPrevious(deployPath, env.ReleaseID)
	})
	phases = append(phases, phase)
	if phase.Status == "failed" {
		logger.Warn("snapshot failed, continuing", zap.String("detail", phase.Detail))
		// Non-fatal: continue even if snapshot fails
	}

	// Phase 7: Swap staging → real (already done in write phase for P1)
	phases = append(phases, agentprotocol.PhaseReport{Phase: "swap", Status: "succeeded", Detail: "inline with write phase"})

	// Phase 8: nginx reload
	if hasNginx && env.AllowReload {
		phase = runPhase("reload", func() error {
			return runNginxReload()
		})
		phases = append(phases, phase)
		if phase.Status == "failed" {
			return failReport(report, phases, start, phase.Detail)
		}
	} else {
		phases = append(phases, agentprotocol.PhaseReport{Phase: "reload", Status: "skipped"})
	}

	// Phase 9: Local verify
	if env.Verify.Enabled {
		phase = runPhase("local_verify", func() error {
			return localVerify(ctx, client, env.Verify)
		})
		phases = append(phases, phase)
		if phase.Status == "failed" {
			return failReport(report, phases, start, phase.Detail)
		}
	} else {
		phases = append(phases, agentprotocol.PhaseReport{Phase: "local_verify", Status: "skipped"})
	}

	report.Phases = phases
	report.DurationMs = time.Since(start).Milliseconds()
	logger.Info("task completed",
		zap.String("task_id", env.TaskID),
		zap.String("status", report.Status),
		zap.Int64("duration_ms", report.DurationMs),
	)

	return report
}

// runPhase executes a phase function and captures timing + error info.
func runPhase(name string, fn func() error) agentprotocol.PhaseReport {
	start := time.Now()
	err := fn()
	duration := time.Since(start).Milliseconds()

	if err != nil {
		return agentprotocol.PhaseReport{
			Phase:      name,
			Status:     "failed",
			DurationMs: duration,
			Detail:     err.Error(),
		}
	}
	return agentprotocol.PhaseReport{
		Phase:      name,
		Status:     "succeeded",
		DurationMs: duration,
	}
}

func failReport(report *agentprotocol.TaskReport, phases []agentprotocol.PhaseReport, start time.Time, errMsg string) *agentprotocol.TaskReport {
	report.Status = agentprotocol.TaskStatusFailed
	report.Phases = phases
	report.DurationMs = time.Since(start).Milliseconds()
	report.Error = errMsg
	return report
}

// writeArtifactFiles copies rendered files from staging to their deploy targets.
func writeArtifactFiles(stagingDir, deployPath, nginxPath string, env *agentprotocol.TaskEnvelope) error {
	for _, mf := range env.Manifest.Files {
		srcPath := filepath.Join(stagingDir, mf.Path)

		var destPath string
		// Route files to correct destination based on prefix
		switch {
		case len(mf.Path) > 5 && mf.Path[:5] == "html/":
			destPath = filepath.Join(deployPath, mf.Path[5:])
		case len(mf.Path) > 6 && mf.Path[:6] == "nginx/":
			destPath = filepath.Join(nginxPath, mf.Path[6:])
		default:
			destPath = filepath.Join(deployPath, mf.Path)
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", destPath, err)
		}

		if err := copyFile(srcPath, destPath); err != nil {
			return fmt.Errorf("copy %s → %s: %w", mf.Path, destPath, err)
		}
	}
	return nil
}

// localVerify performs an HTTP check against localhost to verify deployment.
func localVerify(ctx context.Context, client *http.Client, cfg agentprotocol.VerifyConfig) error {
	if cfg.URL == "" {
		return nil
	}

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	verifyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(verifyCtx, http.MethodHead, cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("create verify request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("verify request: %w", err)
	}
	resp.Body.Close()

	expectedStatus := cfg.StatusCode
	if expectedStatus == 0 {
		expectedStatus = 200
	}

	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("verify: expected status %d, got %d", expectedStatus, resp.StatusCode)
	}

	return nil
}
