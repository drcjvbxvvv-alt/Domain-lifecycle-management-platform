package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"go.uber.org/zap"

	"domain-platform/pkg/agentprotocol"
)

// register performs initial registration with the control plane.
// POST /agent/v1/register
// Returns the assigned agent_id on success.
func register(client *http.Client, baseURL string, req agentprotocol.RegisterRequest, logger *zap.Logger) (*agentprotocol.RegisterResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal register request: %w", err)
	}

	resp, err := client.Post(baseURL+"/agent/v1/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("register POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("register: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Code    int                             `json:"code"`
		Data    agentprotocol.RegisterResponse  `json:"data"`
		Message string                          `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode register response: %w", err)
	}

	logger.Info("registered with control plane",
		zap.String("agent_id", result.Data.AgentID),
		zap.String("status", result.Data.Status),
	)

	return &result.Data, nil
}

// getHostname returns the system hostname, with a fallback.
func getHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}
