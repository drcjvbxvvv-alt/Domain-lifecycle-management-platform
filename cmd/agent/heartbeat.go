package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"domain-platform/pkg/agentprotocol"
)

// heartbeatLoop sends periodic heartbeats to the control plane.
// It runs until the context is cancelled.
func heartbeatLoop(ctx context.Context, client *http.Client, baseURL, agentID string, intervalSecs int, logger *zap.Logger) {
	if intervalSecs <= 0 {
		intervalSecs = 15
	}
	ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer ticker.Stop()

	failCount := 0
	const maxBackoff = 60 // seconds

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := sendHeartbeat(ctx, client, baseURL, agentID, logger)
			if err != nil {
				failCount++
				backoff := intervalSecs * failCount
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				logger.Warn("heartbeat failed",
					zap.Error(err),
					zap.Int("fail_count", failCount),
					zap.Int("backoff_secs", backoff),
				)
				ticker.Reset(time.Duration(backoff) * time.Second)
			} else {
				if failCount > 0 {
					logger.Info("heartbeat recovered", zap.Int("prev_failures", failCount))
					ticker.Reset(time.Duration(intervalSecs) * time.Second)
				}
				failCount = 0
			}
		}
	}
}

func sendHeartbeat(ctx context.Context, client *http.Client, baseURL, agentID string, logger *zap.Logger) error {
	req := agentprotocol.HeartbeatRequest{
		AgentID:      agentID,
		Status:       "online",
		AgentVersion: agentVersion,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/agent/v1/heartbeat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create heartbeat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("heartbeat POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Data agentprotocol.HeartbeatResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode heartbeat response: %w", err)
	}

	if result.Data.HasNewTask {
		logger.Debug("control plane indicates new task available")
	}

	return nil
}
