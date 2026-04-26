// Package dns contract tests verify that provider implementations satisfy the
// Provider interface at compile time, and that the registry round-trip works.
package dns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Compile-time interface assertions ─────────────────────────────────────────

// These lines will fail to compile if any implementation is missing a method.
var (
	_ Provider = (*cloudflareProvider)(nil)
)

// ── Registry round-trip ───────────────────────────────────────────────────────

func TestRegistry_CloudflareRegistered(t *testing.T) {
	types := RegisteredTypes()
	found := false
	for _, ty := range types {
		if ty == "cloudflare" {
			found = true
			break
		}
	}
	assert.True(t, found, "cloudflare provider should be registered via init()")
}

func TestRegistry_GetUnknownProvider(t *testing.T) {
	_, err := Get("no-such-provider", json.RawMessage(`{}`), json.RawMessage(`{}`))
	assert.ErrorIs(t, err, ErrProviderNotRegistered)
}

func TestRegistry_GetCloudflare_MissingZoneID(t *testing.T) {
	_, err := Get("cloudflare",
		json.RawMessage(`{}`),
		json.RawMessage(`{"api_token":"tok"}`),
	)
	assert.ErrorIs(t, err, ErrMissingConfig)
}

func TestRegistry_GetCloudflare_MissingToken(t *testing.T) {
	_, err := Get("cloudflare",
		json.RawMessage(`{"zone_id":"z1"}`),
		json.RawMessage(`{}`),
	)
	assert.ErrorIs(t, err, ErrMissingCredentials)
}

func TestRegistry_GetCloudflare_Valid(t *testing.T) {
	p, err := Get("cloudflare",
		json.RawMessage(`{"zone_id":"z1"}`),
		json.RawMessage(`{"api_token":"tok"}`),
	)
	require.NoError(t, err)
	assert.Equal(t, "cloudflare", p.Name())
}

// ── Full Provider interface exercised via httptest ────────────────────────────

// TestProviderContract_Cloudflare exercises every method of the Provider
// interface against a mock httptest.Server. This ensures the Cloudflare
// implementation satisfies all interface contracts end-to-end.
func TestProviderContract_Cloudflare(t *testing.T) {
	mux := http.NewServeMux()

	// ListRecords
	mux.HandleFunc("GET /zones/z1/dns_records", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"errors":null,"result":[{"id":"r1","type":"A","name":"example.com","content":"1.2.3.4","ttl":300,"proxied":false}]}`))
	})

	// CreateRecord
	mux.HandleFunc("POST /zones/z1/dns_records", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"errors":null,"result":{"id":"new","type":"A","name":"x.example.com","content":"9.9.9.9","ttl":300,"proxied":false}}`))
	})

	// UpdateRecord
	mux.HandleFunc("PUT /zones/z1/dns_records/r1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"errors":null,"result":{"id":"r1","type":"A","name":"example.com","content":"5.5.5.5","ttl":300,"proxied":false}}`))
	})

	// DeleteRecord
	mux.HandleFunc("DELETE /zones/z1/dns_records/r1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"errors":null,"result":{"id":"r1"}}`))
	})

	// GetNameservers
	mux.HandleFunc("GET /zones/z1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"errors":null,"result":{"name_servers":["ns1.cloudflare.com","ns2.cloudflare.com"]}}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := newCloudflareProviderWithClient("z1", "tok", srv.URL, srv.Client())
	ctx := context.Background()

	t.Run("ListRecords", func(t *testing.T) {
		records, err := p.ListRecords(ctx, "z1", RecordFilter{})
		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Equal(t, "r1", records[0].ID)
	})

	t.Run("CreateRecord", func(t *testing.T) {
		rec, err := p.CreateRecord(ctx, "z1", Record{Type: "A", Name: "x.example.com", Content: "9.9.9.9", TTL: 300})
		require.NoError(t, err)
		assert.Equal(t, "new", rec.ID)
	})

	t.Run("UpdateRecord", func(t *testing.T) {
		rec, err := p.UpdateRecord(ctx, "z1", "r1", Record{Type: "A", Name: "example.com", Content: "5.5.5.5", TTL: 300})
		require.NoError(t, err)
		assert.Equal(t, "5.5.5.5", rec.Content)
	})

	t.Run("DeleteRecord", func(t *testing.T) {
		err := p.DeleteRecord(ctx, "z1", "r1")
		require.NoError(t, err)
	})

	t.Run("GetNameservers", func(t *testing.T) {
		ns, err := p.GetNameservers(ctx, "z1")
		require.NoError(t, err)
		assert.Equal(t, []string{"ns1.cloudflare.com", "ns2.cloudflare.com"}, ns)
	})

	t.Run("BatchCreateRecords", func(t *testing.T) {
		// reuse the POST handler — creates two records
		out, err := p.BatchCreateRecords(ctx, "z1", []Record{
			{Type: "A", Name: "x.example.com", Content: "9.9.9.9", TTL: 300},
		})
		require.NoError(t, err)
		assert.Len(t, out, 1)
	})

	t.Run("BatchDeleteRecords", func(t *testing.T) {
		// reuse the DELETE handler
		err := p.BatchDeleteRecords(ctx, "z1", []string{"r1"})
		require.NoError(t, err)
	})
}
