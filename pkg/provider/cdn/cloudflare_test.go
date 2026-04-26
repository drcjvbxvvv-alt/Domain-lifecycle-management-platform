package cdn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compile-time assertion
var _ Provider = (*cloudflareCDNProvider)(nil)

func TestNewCloudflareCDNProvider_MissingCredentials(t *testing.T) {
	tests := []struct {
		name  string
		creds string
	}{
		{"empty JSON", `{}`},
		{"missing zone_id", `{"api_token":"tok"}`},
		{"missing api_token", `{"zone_id":"z123"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCloudflareCDNProvider(json.RawMessage(`{}`), json.RawMessage(tt.creds))
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrMissingCredentials))
		})
	}
}

// cfSuccessResponse wraps a result in Cloudflare's standard success envelope.
func cfSuccessResponse(result any) map[string]any {
	return map[string]any{"success": true, "errors": []any{}, "result": result}
}

func TestCloudflareCDNProvider_AddDomain_NoExistingRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet:
			// findRecordID — no existing record
			json.NewEncoder(w).Encode(cfSuccessResponse([]any{}))
		case r.Method == http.MethodPost:
			// create record
			json.NewEncoder(w).Encode(cfSuccessResponse(map[string]any{
				"id":      "rec-001",
				"type":    "A",
				"name":    "cdn.example.com",
				"content": "1.2.3.4",
				"proxied": true,
			}))
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	p := newCloudflareCDNProviderWithClient("token", "zone123", srv.URL, srv.Client())
	domain, err := p.AddDomain(context.Background(), AddDomainRequest{
		Domain:       "cdn.example.com",
		BusinessType: BusinessTypeWeb,
		Origins:      []Origin{{Address: "1.2.3.4"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "cdn.example.com", domain.Domain)
	assert.Equal(t, DomainStatusOnline, domain.Status)
	assert.Equal(t, "", domain.CNAME) // Cloudflare proxied records have no external CNAME
}

func TestCloudflareCDNProvider_AddDomain_UpdateExisting(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls[r.Method]++
		switch r.Method {
		case http.MethodGet:
			// existing record found
			json.NewEncoder(w).Encode(cfSuccessResponse([]map[string]any{
				{"id": "rec-existing", "name": "cdn.example.com", "type": "A", "proxied": true},
			}))
		case http.MethodPut:
			json.NewEncoder(w).Encode(cfSuccessResponse(map[string]any{"id": "rec-existing"}))
		default:
			http.Error(w, "unexpected "+r.Method, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	p := newCloudflareCDNProviderWithClient("token", "zone123", srv.URL, srv.Client())
	_, err := p.AddDomain(context.Background(), AddDomainRequest{
		Domain:  "cdn.example.com",
		Origins: []Origin{{Address: "5.6.7.8"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls[http.MethodGet])  // findRecordID
	assert.Equal(t, 1, calls[http.MethodPut])  // update (not create)
}

func TestCloudflareCDNProvider_RemoveDomain(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls[r.Method]++
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(cfSuccessResponse([]map[string]any{
				{"id": "rec-001", "name": "cdn.example.com"},
			}))
		case http.MethodDelete:
			json.NewEncoder(w).Encode(cfSuccessResponse(map[string]string{"id": "rec-001"}))
		}
	}))
	defer srv.Close()

	p := newCloudflareCDNProviderWithClient("token", "zone123", srv.URL, srv.Client())
	err := p.RemoveDomain(context.Background(), "cdn.example.com")
	require.NoError(t, err)
	assert.Equal(t, 1, calls[http.MethodGet])
	assert.Equal(t, 1, calls[http.MethodDelete])
}

func TestCloudflareCDNProvider_GetDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfSuccessResponse([]map[string]any{
			{"id": "rec-001", "name": "cdn.example.com", "type": "A", "proxied": true},
		}))
	}))
	defer srv.Close()

	p := newCloudflareCDNProviderWithClient("token", "zone123", srv.URL, srv.Client())
	domain, err := p.GetDomain(context.Background(), "cdn.example.com")
	require.NoError(t, err)
	assert.Equal(t, DomainStatusOnline, domain.Status)
}

func TestCloudflareCDNProvider_GetDomain_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfSuccessResponse([]any{}))
	}))
	defer srv.Close()

	p := newCloudflareCDNProviderWithClient("token", "zone123", srv.URL, srv.Client())
	_, err := p.GetDomain(context.Background(), "missing.example.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDomainNotFound))
}

func TestCloudflareCDNProvider_ListDomains_OnlyProxied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfSuccessResponse([]map[string]any{
			{"id": "r1", "name": "a.example.com", "proxied": true},
			{"id": "r2", "name": "b.example.com", "proxied": false}, // should be excluded
			{"id": "r3", "name": "c.example.com", "proxied": true},
		}))
	}))
	defer srv.Close()

	p := newCloudflareCDNProviderWithClient("token", "zone123", srv.URL, srv.Client())
	domains, err := p.ListDomains(context.Background())
	require.NoError(t, err)
	require.Len(t, domains, 2)
	names := []string{domains[0].Domain, domains[1].Domain}
	assert.Contains(t, names, "a.example.com")
	assert.Contains(t, names, "c.example.com")
}

func TestCloudflareCDNProvider_PurgeURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfSuccessResponse(map[string]string{"id": "purge-sync-id"}))
	}))
	defer srv.Close()

	p := newCloudflareCDNProviderWithClient("token", "zone123", srv.URL, srv.Client())
	task, err := p.PurgeURLs(context.Background(), []string{"https://cdn.example.com/styles.css"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(task.TaskID, cloudflareCDNTaskPrefix))
	assert.Equal(t, TaskStatusDone, task.Status) // Cloudflare purge is synchronous
}

func TestCloudflareCDNProvider_GetTaskStatus_Synthetic(t *testing.T) {
	p := newCloudflareCDNProviderWithClient("token", "zone123", "http://localhost", &http.Client{})
	status, err := p.GetTaskStatus(context.Background(), cloudflareCDNTaskPrefix+"any-id")
	require.NoError(t, err)
	assert.Equal(t, TaskStatusDone, status.Status)
	assert.Equal(t, 100, status.Progress)
	assert.NotNil(t, status.FinishedAt)
}

func TestCloudflareCDNProvider_GetTaskStatus_BadPrefix(t *testing.T) {
	p := newCloudflareCDNProviderWithClient("token", "zone123", "http://localhost", &http.Client{})
	_, err := p.GetTaskStatus(context.Background(), "not-a-cf-task")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTaskNotFound))
}

func TestCloudflareCDNProvider_PrefetchURLs_Unsupported(t *testing.T) {
	p := newCloudflareCDNProviderWithClient("token", "zone123", "http://localhost", &http.Client{})
	_, err := p.PrefetchURLs(context.Background(), []string{"https://cdn.example.com/file"})
	assert.True(t, errors.Is(err, ErrUnsupported))
}

func TestCloudflareCDNProvider_UnsupportedMethods(t *testing.T) {
	p := newCloudflareCDNProviderWithClient("t", "z", "http://localhost", &http.Client{})
	ctx := context.Background()

	_, err := p.GetCacheConfig(ctx, "x")
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorIs(t, p.SetHTTPSConfig(ctx, "x", HTTPSConfig{}), ErrUnsupported)
	_, err = p.GetBandwidthStats(ctx, "x", StatsRequest{})
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestCloudflareCDNProvider_Name(t *testing.T) {
	p := newCloudflareCDNProviderWithClient("t", "z", "http://localhost", &http.Client{})
	assert.Equal(t, "cloudflare_cdn", p.Name())
}
