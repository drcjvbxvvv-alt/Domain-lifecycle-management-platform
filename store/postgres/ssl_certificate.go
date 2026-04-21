package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type SSLCertificate struct {
	ID           int64      `db:"id"`
	UUID         string     `db:"uuid"`
	DomainID     int64      `db:"domain_id"`
	Issuer       *string    `db:"issuer"`
	CertType     *string    `db:"cert_type"`
	SerialNumber *string    `db:"serial_number"`
	IssuedAt     *time.Time `db:"issued_at"`
	ExpiresAt    time.Time  `db:"expires_at"`
	AutoRenew    bool       `db:"auto_renew"`
	Status       string     `db:"status"`
	LastCheckAt  *time.Time `db:"last_check_at"`
	Notes        *string    `db:"notes"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
	DeletedAt    *time.Time `db:"deleted_at"`
}

var ErrSSLCertNotFound = errors.New("ssl certificate not found")

type SSLCertificateStore struct {
	db *sqlx.DB
}

func NewSSLCertificateStore(db *sqlx.DB) *SSLCertificateStore {
	return &SSLCertificateStore{db: db}
}

const sslColumns = `id, uuid, domain_id, issuer, cert_type, serial_number, issued_at, expires_at,
	auto_renew, status, last_check_at, notes, created_at, updated_at, deleted_at`

func (s *SSLCertificateStore) Create(ctx context.Context, cert *SSLCertificate) (*SSLCertificate, error) {
	var out SSLCertificate
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO ssl_certificates (domain_id, issuer, cert_type, serial_number, issued_at, expires_at, auto_renew, status, last_check_at, notes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING `+sslColumns,
		cert.DomainID, cert.Issuer, cert.CertType, cert.SerialNumber,
		cert.IssuedAt, cert.ExpiresAt, cert.AutoRenew, cert.Status, cert.LastCheckAt, cert.Notes,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("create ssl certificate: %w", err)
	}
	return &out, nil
}

func (s *SSLCertificateStore) GetByID(ctx context.Context, id int64) (*SSLCertificate, error) {
	var cert SSLCertificate
	err := s.db.GetContext(ctx, &cert,
		`SELECT `+sslColumns+` FROM ssl_certificates WHERE id = $1 AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSSLCertNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get ssl certificate: %w", err)
	}
	return &cert, nil
}

func (s *SSLCertificateStore) ListByDomain(ctx context.Context, domainID int64) ([]SSLCertificate, error) {
	var certs []SSLCertificate
	err := s.db.SelectContext(ctx, &certs,
		`SELECT `+sslColumns+` FROM ssl_certificates
		 WHERE domain_id = $1 AND deleted_at IS NULL
		 ORDER BY expires_at DESC`, domainID)
	if err != nil {
		return nil, fmt.Errorf("list ssl certificates: %w", err)
	}
	return certs, nil
}

// Upsert inserts or updates a certificate based on domain_id + serial_number.
func (s *SSLCertificateStore) Upsert(ctx context.Context, cert *SSLCertificate) (*SSLCertificate, error) {
	var out SSLCertificate
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO ssl_certificates (domain_id, issuer, cert_type, serial_number, issued_at, expires_at, auto_renew, status, last_check_at, notes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (domain_id, serial_number) WHERE deleted_at IS NULL
		 DO UPDATE SET issuer = EXCLUDED.issuer, expires_at = EXCLUDED.expires_at,
		   status = EXCLUDED.status, last_check_at = EXCLUDED.last_check_at, updated_at = NOW()
		 RETURNING `+sslColumns,
		cert.DomainID, cert.Issuer, cert.CertType, cert.SerialNumber,
		cert.IssuedAt, cert.ExpiresAt, cert.AutoRenew, cert.Status, cert.LastCheckAt, cert.Notes,
	).StructScan(&out)
	if err != nil {
		return nil, fmt.Errorf("upsert ssl certificate: %w", err)
	}
	return &out, nil
}

func (s *SSLCertificateStore) UpdateStatus(ctx context.Context, id int64, status string, lastCheckAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ssl_certificates SET status = $2, last_check_at = $3, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`, id, status, lastCheckAt)
	if err != nil {
		return fmt.Errorf("update ssl cert status: %w", err)
	}
	return nil
}

// ListExpiring returns certificates expiring within the given number of days.
func (s *SSLCertificateStore) ListExpiring(ctx context.Context, days int) ([]SSLCertificate, error) {
	var certs []SSLCertificate
	err := s.db.SelectContext(ctx, &certs,
		`SELECT `+sslColumns+` FROM ssl_certificates
		 WHERE deleted_at IS NULL
		   AND expires_at <= CURRENT_TIMESTAMP + $1 * INTERVAL '1 day'
		   AND status != 'revoked'
		 ORDER BY expires_at ASC`, days)
	if err != nil {
		return nil, fmt.Errorf("list expiring ssl certs: %w", err)
	}
	return certs, nil
}

func (s *SSLCertificateStore) SoftDelete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ssl_certificates SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete ssl certificate: %w", err)
	}
	return nil
}
