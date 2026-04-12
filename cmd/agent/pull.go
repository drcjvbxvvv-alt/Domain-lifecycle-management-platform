package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"domain-platform/pkg/agentprotocol"
)

// pullLoop long-polls the control plane for tasks and executes them.
// It runs until the context is cancelled.
func pullLoop(ctx context.Context, client *http.Client, baseURL, agentID string, cfg agentConfig, logger *zap.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		envelope, err := pollNextTask(ctx, client, baseURL, agentID)
		if err != nil {
			logger.Debug("poll for task", zap.Error(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		if envelope == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}

		logger.Info("received task",
			zap.String("task_id", envelope.TaskID),
			zap.String("type", envelope.Type),
			zap.Int("domain_count", len(envelope.Domains)),
		)

		// Claim the task
		if err := claimTask(ctx, client, baseURL, envelope.TaskID); err != nil {
			logger.Error("claim task", zap.Error(err), zap.String("task_id", envelope.TaskID))
			continue
		}

		// Execute the task
		report := handleTask(ctx, client, envelope, cfg, logger)

		// Report result
		if err := reportTask(ctx, client, baseURL, report); err != nil {
			logger.Error("report task", zap.Error(err), zap.String("task_id", envelope.TaskID))
		}
	}
}

// pollNextTask long-polls GET /agent/v1/tasks for the next available task.
func pollNextTask(ctx context.Context, client *http.Client, baseURL, agentID string) (*agentprotocol.TaskEnvelope, error) {
	pollCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(pollCtx, http.MethodGet, baseURL+"/agent/v1/tasks?agent_id="+agentID, nil)
	if err != nil {
		return nil, fmt.Errorf("create poll request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("poll: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Data agentprotocol.TaskEnvelope `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode task envelope: %w", err)
	}

	return &result.Data, nil
}

// claimTask tells the control plane the agent has claimed a task.
func claimTask(ctx context.Context, client *http.Client, baseURL, taskID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/agent/v1/tasks/"+taskID+"/claim", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("claim POST: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("claim: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// reportTask sends the task completion report to the control plane.
func reportTask(ctx context.Context, client *http.Client, baseURL string, report *agentprotocol.TaskReport) error {
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/agent/v1/tasks/"+report.TaskID+"/report", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("report POST: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("report: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// downloadArtifact downloads individual artifact files from S3 based on the manifest.
func downloadArtifact(ctx context.Context, client *http.Client, artifactURL, destDir string, manifest *agentprotocol.Manifest) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	// In Phase 1, the artifact URL is a base prefix. Each file is at prefix/path.
	// For simplicity, we download files listed in the manifest.
	for _, mf := range manifest.Files {
		fileURL := artifactURL + "/" + mf.Path
		destPath := filepath.Join(destDir, mf.Path)

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", mf.Path, err)
		}

		if err := downloadFile(ctx, client, fileURL, destPath); err != nil {
			return fmt.Errorf("download %s: %w", mf.Path, err)
		}
	}

	return nil
}

func downloadFile(ctx context.Context, client *http.Client, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}
