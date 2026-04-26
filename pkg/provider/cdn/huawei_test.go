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
var _ Provider = (*huaweiCDNProvider)(nil)

func TestNewHuaweiCDNProvider_MissingCredentials(t *testing.T) {
	tests := []struct {
		name  string
		creds string
	}{
		{"empty JSON", `{}`},
		{"missing secret_key", `{"access_key":"ak"}`},
		{"missing access_key", `{"secret_key":"sk"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHuaweiCDNProvider(json.RawMessage(`{}`), json.RawMessage(tt.creds))
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrMissingCredentials))
		})
	}
}

func TestHuaweiCDNProvider_AddDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cdn/domains") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"domain": map[string]any{
					"id":            "domain-id-001",
					"domain_name":   "cdn.example.com",
					"cname":         "cdn.example.com.c.cdnhwc1.com",
					"domain_status": "configuring",
					"business_type": "web",
					"create_time":   1704067200000,
				},
			})
			return
		}
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer srv.Close()

	p := newHuaweiCDNProviderWithClient("ak", "sk", srv.URL, srv.Client())
	domain, err := p.AddDomain(context.Background(), AddDomainRequest{
		Domain:       "cdn.example.com",
		BusinessType: BusinessTypeWeb,
		Origins:      []Origin{{Address: "1.2.3.4"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "cdn.example.com", domain.Domain)
	assert.Equal(t, "cdn.example.com.c.cdnhwc1.com", domain.CNAME)
	assert.Equal(t, DomainStatusConfiguring, domain.Status)
	assert.NotNil(t, domain.CreatedAt)
}

func TestHuaweiCDNProvider_GetDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/cdn/domains") && r.URL.Query().Get("domain_name") == "cdn.example.com":
			// resolveDomainID list call
			json.NewEncoder(w).Encode(map[string]any{
				"domains": []map[string]any{
					{
						"id":            "domain-id-001",
						"domain_name":   "cdn.example.com",
						"cname":         "cdn.example.com.c.cdnhwc1.com",
						"domain_status": "online",
						"business_type": "web",
					},
				},
				"total": 1,
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/detail"):
			// GetDomain detail call
			json.NewEncoder(w).Encode(map[string]any{
				"domain": map[string]any{
					"id":            "domain-id-001",
					"domain_name":   "cdn.example.com",
					"cname":         "cdn.example.com.c.cdnhwc1.com",
					"domain_status": "online",
					"business_type": "web",
				},
			})
		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	p := newHuaweiCDNProviderWithClient("ak", "sk", srv.URL, srv.Client())
	domain, err := p.GetDomain(context.Background(), "cdn.example.com")
	require.NoError(t, err)
	assert.Equal(t, "cdn.example.com", domain.Domain)
	assert.Equal(t, DomainStatusOnline, domain.Status)
}

func TestHuaweiCDNProvider_GetDomain_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// resolveDomainID returns empty list
		json.NewEncoder(w).Encode(map[string]any{
			"domains": []any{},
			"total":   0,
		})
	}))
	defer srv.Close()

	p := newHuaweiCDNProviderWithClient("ak", "sk", srv.URL, srv.Client())
	_, err := p.GetDomain(context.Background(), "missing.example.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDomainNotFound))
}

func TestHuaweiCDNProvider_RemoveDomain(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		key := r.Method + " " + r.URL.Path
		// Strip base path for matching
		if strings.Contains(r.URL.Path, "?domain_name=") || r.URL.Query().Get("domain_name") != "" {
			calls["list"]++
			json.NewEncoder(w).Encode(map[string]any{
				"domains": []map[string]any{{"id": "dom-001", "domain_name": "cdn.example.com"}},
				"total":   1,
			})
			return
		}
		calls[key]++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	p := newHuaweiCDNProviderWithClient("ak", "sk", srv.URL, srv.Client())
	err := p.RemoveDomain(context.Background(), "cdn.example.com")
	require.NoError(t, err)
	assert.Equal(t, 1, calls["list"])
}

func TestHuaweiCDNProvider_PurgeURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"refresh_task": map[string]any{
				"id":     "refresh-001",
				"status": "task_inited",
			},
		})
	}))
	defer srv.Close()

	p := newHuaweiCDNProviderWithClient("ak", "sk", srv.URL, srv.Client())
	task, err := p.PurgeURLs(context.Background(), []string{"https://cdn.example.com/img.png"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(task.TaskID, huaweiCDNRefreshTaskPrefix))
	assert.Equal(t, TaskStatusPending, task.Status)
}

func TestHuaweiCDNProvider_PrefetchURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"preheating_task": map[string]any{
				"id":     "preheat-001",
				"status": "task_inited",
			},
		})
	}))
	defer srv.Close()

	p := newHuaweiCDNProviderWithClient("ak", "sk", srv.URL, srv.Client())
	task, err := p.PrefetchURLs(context.Background(), []string{"https://cdn.example.com/large.bin"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(task.TaskID, huaweiCDNPreheatTaskPrefix))
}

func TestHuaweiCDNProvider_GetTaskStatus_Refresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"refresh_task": map[string]any{
				"id":          "refresh-001",
				"status":      "task_done",
				"create_time": 1704067200000,
				"finish_time": 1704067260000,
			},
		})
	}))
	defer srv.Close()

	p := newHuaweiCDNProviderWithClient("ak", "sk", srv.URL, srv.Client())
	status, err := p.GetTaskStatus(context.Background(), huaweiCDNRefreshTaskPrefix+"refresh-001")
	require.NoError(t, err)
	assert.Equal(t, TaskStatusDone, status.Status)
	assert.NotNil(t, status.FinishedAt)
	assert.Equal(t, 100, status.Progress)
}

func TestHuaweiCDNProvider_GetTaskStatus_UnknownPrefix(t *testing.T) {
	p := newHuaweiCDNProviderWithClient("ak", "sk", "http://localhost", &http.Client{})
	_, err := p.GetTaskStatus(context.Background(), "unknown-task-id")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTaskNotFound))
}

func TestHuaweiCDNProvider_UnsupportedMethods(t *testing.T) {
	p := newHuaweiCDNProviderWithClient("a", "s", "http://localhost", &http.Client{})
	ctx := context.Background()

	_, err := p.GetOriginConfig(ctx, "x")
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorIs(t, p.SetCacheConfig(ctx, "x", CacheConfig{}), ErrUnsupported)
	_, err = p.GetTrafficStats(ctx, "x", StatsRequest{})
	assert.ErrorIs(t, err, ErrUnsupported)
}
