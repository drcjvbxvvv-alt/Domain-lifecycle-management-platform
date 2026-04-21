package ssl

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

var (
	ErrNotFound    = errors.New("ssl certificate not found")
	ErrCheckFailed = errors.New("tls check failed")
)

// StatusActive, StatusExpiring, StatusExpired mirror the ssl_certificates.status CHECK constraint.
const (
	StatusActive   = "active"
	StatusExpiring = "expiring"
	StatusExpired  = "expired"
	StatusRevoked  = "revoked"
)

// ExpiryThreshold30d is the window for "expiring" status.
const ExpiryThreshold30d = 30 * 24 * time.Hour

// ComputeSSLStatus returns "expired", "expiring", or "active" based on the
// certificate's expiry time relative to now.
func ComputeSSLStatus(expiresAt time.Time) string {
	return computeSSLStatusAt(expiresAt, time.Now())
}

// computeSSLStatusAt is the testable inner function — now is injected.
func computeSSLStatusAt(expiresAt, now time.Time) string {
	if !now.Before(expiresAt) {
		return StatusExpired
	}
	if expiresAt.Sub(now) <= ExpiryThreshold30d {
		return StatusExpiring
	}
	return StatusActive
}

// Service wraps SSLCertificateStore + DomainStore with business logic.
type Service struct {
	store       *postgres.SSLCertificateStore
	domainStore *postgres.DomainStore
	logger      *zap.Logger
}

func NewService(
	store *postgres.SSLCertificateStore,
	domainStore *postgres.DomainStore,
	logger *zap.Logger,
) *Service {
	return &Service{store: store, domainStore: domainStore, logger: logger}
}

// ── Manual CRUD ──────────────────────────────────────────────────────────────

type CreateInput struct {
	DomainID     int64
	Issuer       *string
	CertType     *string
	SerialNumber *string
	IssuedAt     *time.Time
	ExpiresAt    time.Time
	AutoRenew    bool
	Notes        *string
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*postgres.SSLCertificate, error) {
	status := ComputeSSLStatus(in.ExpiresAt)
	now := time.Now()
	cert := &postgres.SSLCertificate{
		DomainID:     in.DomainID,
		Issuer:       in.Issuer,
		CertType:     in.CertType,
		SerialNumber: in.SerialNumber,
		IssuedAt:     in.IssuedAt,
		ExpiresAt:    in.ExpiresAt,
		AutoRenew:    in.AutoRenew,
		Status:       status,
		LastCheckAt:  &now,
		Notes:        in.Notes,
	}
	created, err := s.store.Create(ctx, cert)
	if err != nil {
		return nil, fmt.Errorf("create ssl cert: %w", err)
	}
	s.logger.Info("ssl cert created",
		zap.Int64("cert_id", created.ID),
		zap.Int64("domain_id", in.DomainID),
		zap.String("status", status),
	)
	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id int64) (*postgres.SSLCertificate, error) {
	cert, err := s.store.GetByID(ctx, id)
	if errors.Is(err, postgres.ErrSSLCertNotFound) {
		return nil, ErrNotFound
	}
	return cert, err
}

func (s *Service) List(ctx context.Context, domainID int64) ([]postgres.SSLCertificate, error) {
	return s.store.ListByDomain(ctx, domainID)
}

func (s *Service) ListExpiring(ctx context.Context, days int) ([]postgres.SSLCertificate, error) {
	if days <= 0 {
		days = 30
	}
	return s.store.ListExpiring(ctx, days)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	// Verify exists first so we return a typed error.
	if _, err := s.store.GetByID(ctx, id); err != nil {
		if errors.Is(err, postgres.ErrSSLCertNotFound) {
			return ErrNotFound
		}
		return err
	}
	return s.store.SoftDelete(ctx, id)
}

// ── TLS Probe ────────────────────────────────────────────────────────────────

// CheckExpiry connects to fqdn:443, extracts the leaf certificate, computes
// status, and upserts the result into ssl_certificates. Returns the upserted row.
func (s *Service) CheckExpiry(ctx context.Context, domainID int64, fqdn string) (*postgres.SSLCertificate, error) {
	info, err := probeTLS(ctx, fqdn)
	if err != nil {
		s.logger.Warn("tls probe failed",
			zap.Int64("domain_id", domainID),
			zap.String("fqdn", fqdn),
			zap.Error(err),
		)
		return nil, fmt.Errorf("%w: %s: %s", ErrCheckFailed, fqdn, err.Error())
	}

	status := ComputeSSLStatus(info.expiresAt)
	now := time.Now()

	cert := &postgres.SSLCertificate{
		DomainID:     domainID,
		Issuer:       &info.issuer,
		CertType:     strPtr("dv"), // we don't detect EV/OV from TLS alone
		SerialNumber: &info.serial,
		IssuedAt:     &info.issuedAt,
		ExpiresAt:    info.expiresAt,
		AutoRenew:    false,
		Status:       status,
		LastCheckAt:  &now,
	}

	upserted, err := s.store.Upsert(ctx, cert)
	if err != nil {
		return nil, fmt.Errorf("upsert ssl cert: %w", err)
	}

	s.logger.Info("ssl cert checked",
		zap.Int64("domain_id", domainID),
		zap.String("fqdn", fqdn),
		zap.String("status", status),
		zap.Time("expires_at", info.expiresAt),
	)
	return upserted, nil
}

// CheckAllActive fetches every active domain and calls CheckExpiry for each.
// Errors are logged but do not abort the batch — returns the number of failures.
func (s *Service) CheckAllActive(ctx context.Context) (checked, failed int) {
	domains, err := s.domainStore.ListWithFilter(ctx, postgres.ListFilter{
		LifecycleState: strPtr("active"),
		Limit:          10000,
	})
	if err != nil {
		s.logger.Error("ssl batch check: list active domains", zap.Error(err))
		return 0, 0
	}

	for _, d := range domains {
		if _, err := s.CheckExpiry(ctx, d.ID, d.FQDN); err != nil {
			s.logger.Warn("ssl batch check: probe failed",
				zap.Int64("domain_id", d.ID),
				zap.String("fqdn", d.FQDN),
				zap.Error(err),
			)
			failed++
		} else {
			checked++
		}
	}
	return checked, failed
}

// ── TLS probe helpers ────────────────────────────────────────────────────────

type certInfo struct {
	issuer    string
	serial    string
	issuedAt  time.Time
	expiresAt time.Time
}

// probeTLS opens a TLS connection to host:443, validates the cert chain, and
// returns the leaf certificate metadata. Uses the system cert pool so only
// publicly-trusted CAs pass. Context deadline/timeout is respected.
func probeTLS(ctx context.Context, fqdn string) (*certInfo, error) {
	// Strip any trailing dot (FQDN canonical form)
	host := strings.TrimRight(fqdn, ".")

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{},
		Config: &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: false, //nolint:gosec // intentional — we want real cert validation
		},
	}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return nil, err
	}
	defer conn.Close() //nolint:errcheck

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return nil, fmt.Errorf("not a TLS connection")
	}

	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("no peer certificates in TLS handshake")
	}

	leaf := state.PeerCertificates[0]
	return &certInfo{
		issuer:    issuerCommonName(leaf),
		serial:    hex.EncodeToString(leaf.SerialNumber.Bytes()),
		issuedAt:  leaf.NotBefore,
		expiresAt: leaf.NotAfter,
	}, nil
}

// issuerCommonName returns the issuer CN, falling back to O[0] if CN is empty.
func issuerCommonName(cert *x509.Certificate) string {
	if cert.Issuer.CommonName != "" {
		return cert.Issuer.CommonName
	}
	if len(cert.Issuer.Organization) > 0 {
		return cert.Issuer.Organization[0]
	}
	return "unknown"
}

func strPtr(s string) *string { return &s }
