// cmd/agent is the Pull Agent binary deployed to each Nginx host.
//
// Safety boundary (CLAUDE.md Critical Rule #3):
//   - Only hard-coded shell-out points are permitted (nginx -t, nginx -s reload,
//     configured local-verify HTTP, systemd self-restart).
//   - Variable os/exec calls, plugin.Open, and reflect.Call are FORBIDDEN.
//   - CI gate: `make check-agent-safety` enforces this structurally.
//   - Any new os/exec call site MUST have a // safe: comment with Opus review approval.
//
// Opus review is REQUIRED for any PR that modifies cmd/agent/.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"domain-platform/internal/bootstrap"
	"domain-platform/pkg/agentprotocol"
)

const agentVersion = "0.1.0"

func main() {
	cfg, err := bootstrap.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := bootstrap.NewLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	agentCfg := cfg.Agent
	if agentCfg.ControlPlaneURL == "" {
		logger.Fatal("agent.control_plane_url is required")
	}

	httpClient, err := buildAgentHTTPClient(agentCfg)
	if err != nil {
		logger.Fatal("build HTTP client", zap.Error(err))
	}

	// Build runtime config for task handler
	runtimeCfg := agentConfig{
		DeployPath:  agentCfg.DeployPath,
		NginxPath:   agentCfg.NginxPath,
		StagingPath: agentCfg.StagingPath,
		SigningKey:   agentCfg.SigningKey,
		AllowReload: agentCfg.AllowReload,
	}

	logger.Info("agent starting",
		zap.String("version", agentVersion),
		zap.String("control_plane", agentCfg.ControlPlaneURL),
		zap.String("deploy_path", runtimeCfg.DeployPath),
		zap.String("nginx_path", runtimeCfg.NginxPath),
	)

	// ── Register with control plane ──────────────────────────────────────
	regReq := agentprotocol.RegisterRequest{
		Hostname:     getHostname(),
		AgentVersion: agentVersion,
		Region:       agentCfg.Region,
	}

	regResp, err := register(httpClient, agentCfg.ControlPlaneURL, regReq, logger)
	if err != nil {
		logger.Fatal("register with control plane", zap.Error(err))
	}

	agentID := regResp.AgentID
	logger.Info("registered",
		zap.String("agent_id", agentID),
	)

	// ── Start loops ──────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Heartbeat loop (background goroutine)
	go heartbeatLoop(ctx, httpClient, agentCfg.ControlPlaneURL, agentID, agentCfg.HeartbeatSecs, logger)

	// Pull loop (background goroutine)
	go pullLoop(ctx, httpClient, agentCfg.ControlPlaneURL, agentID, runtimeCfg, logger)

	// ── Wait for shutdown signal ─────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Info("shutting down", zap.String("signal", sig.String()))
	cancel()

	// Give goroutines a moment to finish
	time.Sleep(2 * time.Second)
	logger.Info("agent exited cleanly")
}

// buildAgentHTTPClient returns an *http.Client configured for mTLS when cert
// files are present, or plain HTTPS (no client cert) for development.
func buildAgentHTTPClient(cfg bootstrap.AgentConfig) (*http.Client, error) {
	transport := &http.Transport{
		TLSHandshakeTimeout: 10 * time.Second,
	}

	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load agent cert: %w", err)
		}

		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
		}

		if cfg.CACertFile != "" {
			caCert, err := os.ReadFile(cfg.CACertFile)
			if err != nil {
				return nil, fmt.Errorf("read CA cert: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("parse CA cert")
			}
			tlsCfg.RootCAs = pool
		}

		transport.TLSClientConfig = tlsCfg
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}
