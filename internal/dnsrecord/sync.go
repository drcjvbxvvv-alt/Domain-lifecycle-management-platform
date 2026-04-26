package dnsrecord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"

	dnsprovider "domain-platform/pkg/provider/dns"
	"domain-platform/store/postgres"
)

// SyncService provides DNS record sync and local CRUD backed by domain_dns_records.
// CRUD operations follow the write-through pattern:
//   1. Call the provider API.
//   2. On success, persist to the local DB.
//   3. On provider failure, return an error without touching the local DB.
type SyncService struct {
	dnsProviders *postgres.DNSProviderStore
	domains      *postgres.DomainStore
	records      *postgres.DomainDNSRecordStore
	logger       *zap.Logger
}

// NewSyncService creates a SyncService.
func NewSyncService(
	dnsProviders *postgres.DNSProviderStore,
	domains *postgres.DomainStore,
	records *postgres.DomainDNSRecordStore,
	logger *zap.Logger,
) *SyncService {
	return &SyncService{
		dnsProviders: dnsProviders,
		domains:      domains,
		records:      records,
		logger:       logger,
	}
}

// ── SyncResult ────────────────────────────────────────────────────────────────

// SyncResult summarises the outcome of a sync operation.
type SyncResult struct {
	Upserted int
	Deleted  int
}

// ── Sync ─────────────────────────────────────────────────────────────────────

// Sync pulls all DNS records from the provider for the given domain and
// upserts them into domain_dns_records. Records that no longer exist on the
// provider side are soft-deleted locally.
func (s *SyncService) Sync(ctx context.Context, domain *postgres.Domain) (*SyncResult, error) {
	p, zoneID, dnsProv, err := s.resolveSync(ctx, domain)
	if err != nil {
		return nil, err
	}

	records, err := p.ListRecords(ctx, zoneID, dnsprovider.RecordFilter{})
	if err != nil {
		return nil, fmt.Errorf("list records from %s: %w", p.Name(), err)
	}

	result := &SyncResult{}
	providerIDs := make([]string, 0, len(records))

	for _, rec := range records {
		row := providerRecordToRow(domain.ID, dnsProv.ID, rec)
		if _, _, err := s.records.UpsertByProviderID(ctx, row); err != nil {
			s.logger.Error("upsert dns record during sync",
				zap.String("fqdn", domain.FQDN),
				zap.String("record_id", rec.ID),
				zap.Error(err),
			)
			continue
		}
		result.Upserted++
		if rec.ID != "" {
			providerIDs = append(providerIDs, rec.ID)
		}
	}

	// Soft-delete local records not seen in provider response.
	deleted, err := s.records.DeleteByProviderIDs(ctx, domain.ID, providerIDs)
	if err != nil {
		s.logger.Error("prune stale dns records",
			zap.String("fqdn", domain.FQDN),
			zap.Error(err),
		)
	}
	result.Deleted = int(deleted)

	s.logger.Info("dns record sync complete",
		zap.String("fqdn", domain.FQDN),
		zap.String("provider", p.Name()),
		zap.Int("upserted", result.Upserted),
		zap.Int("deleted", result.Deleted),
	)
	return result, nil
}

// ── Local CRUD (write-through) ────────────────────────────────────────────────

// CreateRecord creates a record at the provider and persists it locally.
func (s *SyncService) CreateRecord(ctx context.Context, domain *postgres.Domain, in CreateRecordInput) (*postgres.DomainDNSRecord, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	p, zoneID, dnsProv, err := s.resolveSync(ctx, domain)
	if err != nil {
		return nil, err
	}

	created, err := p.CreateRecord(ctx, zoneID, dnsprovider.Record{
		Type:     in.Type,
		Name:     in.Name,
		Content:  in.Content,
		TTL:      in.TTL,
		Priority: in.Priority,
		Proxied:  in.Proxied,
	})
	if err != nil {
		return nil, fmt.Errorf("create record via %s: %w", p.Name(), err)
	}

	row := providerRecordToRow(domain.ID, dnsProv.ID, *created)
	saved, err := s.records.Create(ctx, row)
	if err != nil {
		s.logger.Error("persist created dns record",
			zap.String("fqdn", domain.FQDN),
			zap.String("provider_record_id", created.ID),
			zap.Error(err),
		)
		return nil, fmt.Errorf("persist dns record: %w", err)
	}

	s.logger.Info("created dns record",
		zap.String("fqdn", domain.FQDN),
		zap.String("type", created.Type),
		zap.String("name", created.Name),
		zap.Int64("local_id", saved.ID),
	)
	return saved, nil
}

// UpdateRecord updates a record at the provider then updates the local row.
func (s *SyncService) UpdateRecord(ctx context.Context, domain *postgres.Domain, localID int64, in UpdateRecordInput) (*postgres.DomainDNSRecord, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	existing, err := s.records.GetByID(ctx, localID)
	if err != nil {
		return nil, err
	}
	if existing.DomainID != domain.ID {
		return nil, ErrDNSRecordNotFound
	}

	p, zoneID, _, err := s.resolveSync(ctx, domain)
	if err != nil {
		return nil, err
	}

	providerRecordID := ""
	if existing.ProviderRecordID != nil {
		providerRecordID = *existing.ProviderRecordID
	}

	updated, err := p.UpdateRecord(ctx, zoneID, providerRecordID, dnsprovider.Record{
		Type:     in.Type,
		Name:     in.Name,
		Content:  in.Content,
		TTL:      in.TTL,
		Priority: in.Priority,
		Proxied:  in.Proxied,
	})
	if err != nil {
		return nil, fmt.Errorf("update record via %s: %w", p.Name(), err)
	}

	// Persist locally.
	existing.Content = updated.Content
	existing.TTL = updated.TTL
	existing.Priority = ptrInt(updated.Priority)
	existing.Proxied = updated.Proxied
	if updated.ID != "" {
		existing.ProviderRecordID = &updated.ID
	}

	saved, err := s.records.Update(ctx, existing)
	if err != nil {
		return nil, fmt.Errorf("persist updated dns record: %w", err)
	}

	s.logger.Info("updated dns record",
		zap.String("fqdn", domain.FQDN),
		zap.Int64("local_id", localID),
	)
	return saved, nil
}

// DeleteRecord deletes a record at the provider then soft-deletes it locally.
func (s *SyncService) DeleteRecord(ctx context.Context, domain *postgres.Domain, localID int64) error {
	existing, err := s.records.GetByID(ctx, localID)
	if err != nil {
		return err
	}
	if existing.DomainID != domain.ID {
		return ErrDNSRecordNotFound
	}

	p, zoneID, _, err := s.resolveSync(ctx, domain)
	if err != nil {
		return err
	}

	providerRecordID := ""
	if existing.ProviderRecordID != nil {
		providerRecordID = *existing.ProviderRecordID
	}

	if err := p.DeleteRecord(ctx, zoneID, providerRecordID); err != nil {
		return fmt.Errorf("delete record via %s: %w", p.Name(), err)
	}

	if err := s.records.SoftDelete(ctx, localID); err != nil {
		s.logger.Error("soft delete dns record after provider success",
			zap.Int64("local_id", localID),
			zap.Error(err),
		)
		return fmt.Errorf("soft delete dns record: %w", err)
	}

	s.logger.Info("deleted dns record",
		zap.String("fqdn", domain.FQDN),
		zap.Int64("local_id", localID),
	)
	return nil
}

// BatchDelete deletes multiple records at the provider then soft-deletes locally.
// Returns the count of successfully deleted records. Partial failures are logged
// but do not abort processing of the remaining records.
func (s *SyncService) BatchDelete(ctx context.Context, domain *postgres.Domain, localIDs []int64) (int, error) {
	if len(localIDs) == 0 {
		return 0, nil
	}

	p, zoneID, _, err := s.resolveSync(ctx, domain)
	if err != nil {
		return 0, err
	}

	// Build provider record IDs.
	providerIDs := make([]string, 0, len(localIDs))
	idToLocalID := map[string]int64{}
	for _, localID := range localIDs {
		row, err := s.records.GetByID(ctx, localID)
		if err != nil {
			s.logger.Warn("batch delete — record not found", zap.Int64("local_id", localID))
			continue
		}
		if row.DomainID != domain.ID {
			s.logger.Warn("batch delete — record not owned by domain",
				zap.Int64("local_id", localID),
				zap.Int64("domain_id", domain.ID),
			)
			continue
		}
		if row.ProviderRecordID != nil && *row.ProviderRecordID != "" {
			providerIDs = append(providerIDs, *row.ProviderRecordID)
			idToLocalID[*row.ProviderRecordID] = localID
		}
	}

	if len(providerIDs) == 0 {
		return 0, nil
	}

	if err := p.BatchDeleteRecords(ctx, zoneID, providerIDs); err != nil {
		return 0, fmt.Errorf("batch delete records via %s: %w", p.Name(), err)
	}

	deleted := 0
	for _, localID := range localIDs {
		if err := s.records.SoftDelete(ctx, localID); err == nil {
			deleted++
		}
	}

	s.logger.Info("batch deleted dns records",
		zap.String("fqdn", domain.FQDN),
		zap.Int("count", deleted),
	)
	return deleted, nil
}

// ListRecords returns the local DNS record snapshot for a domain.
func (s *SyncService) ListRecords(ctx context.Context, domain *postgres.Domain, recordType string) ([]postgres.DomainDNSRecord, error) {
	return s.records.ListByDomain(ctx, domain.ID, strings.ToUpper(recordType))
}

// ── helpers ───────────────────────────────────────────────────────────────────

// resolveSync fetches the domain's DNS provider and returns the provider
// client, zone ID, and the provider DB row.
func (s *SyncService) resolveSync(ctx context.Context, domain *postgres.Domain) (dnsprovider.Provider, string, *postgres.DNSProvider, error) {
	if domain.DNSProviderID == nil {
		return nil, "", nil, ErrNoProvider
	}
	prov, err := s.dnsProviders.GetByID(ctx, *domain.DNSProviderID)
	if err != nil {
		return nil, "", nil, fmt.Errorf("fetch dns provider: %w", err)
	}
	p, err := dnsprovider.Get(prov.ProviderType, prov.Config, prov.Credentials)
	if err != nil {
		return nil, "", nil, fmt.Errorf("%w: %v", ErrProviderInit, err)
	}
	zoneID := extractZoneID(prov.Config)
	return p, zoneID, prov, nil
}

// providerRecordToRow converts a provider Record into a DomainDNSRecord row.
func providerRecordToRow(domainID, providerID int64, rec dnsprovider.Record) *postgres.DomainDNSRecord {
	row := &postgres.DomainDNSRecord{
		DomainID:      domainID,
		DNSProviderID: &providerID,
		RecordType:    rec.Type,
		Name:          rec.Name,
		Content:       rec.Content,
		TTL:           rec.TTL,
		Proxied:       rec.Proxied,
		Extra:         json.RawMessage(`{}`),
	}
	if rec.ID != "" {
		row.ProviderRecordID = &rec.ID
	}
	if rec.Priority > 0 {
		row.Priority = ptrInt(rec.Priority)
	}
	return row
}

func ptrInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// ── ErrDNSRecordNotFound re-export ────────────────────────────────────────────
// So callers can use dnsrecord.ErrDNSRecordNotFound without importing postgres.
var ErrDNSRecordNotFound = postgres.ErrDNSRecordNotFound
