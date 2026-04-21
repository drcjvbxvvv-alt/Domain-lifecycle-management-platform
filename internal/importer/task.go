package importer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"domain-platform/internal/tasks"
)

// HandleDomainImport is the asynq handler for tasks.TypeDomainImport.
// Register it in cmd/worker/main.go via mux.HandleFunc(tasks.TypeDomainImport, ...).
func (s *Service) HandleDomainImport(ctx context.Context, t *asynq.Task) error {
	var payload tasks.DomainImportPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal domain import payload: %w", err)
	}

	s.logger.Info("processing domain import task", zap.Int64("job_id", payload.JobID))

	if err := s.ProcessImportPayload(ctx, payload.JobID); err != nil {
		s.logger.Error("domain import task failed",
			zap.Int64("job_id", payload.JobID),
			zap.Error(err),
		)
		return err
	}

	return nil
}
