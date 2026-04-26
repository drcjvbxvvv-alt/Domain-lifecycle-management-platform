package cdn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compile-time assertion
var _ Provider = (*aliyunCDNProvider)(nil)

func TestNewAliyunCDNProvider_MissingCredentials(t *testing.T) {
	tests := []struct {
		name  string
		creds string
	}{
		{"empty JSON", `{}`},
		{"missing secret", `{"access_key_id":"key"}`},
		{"missing key id", `{"access_key_secret":"secret"}`},
		{"whitespace key", `{"access_key_id":"  ","access_key_secret":"secret"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAliyunCDNProvider(json.RawMessage(`{}`), json.RawMessage(tt.creds))
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrMissingCredentials), "expected ErrMissingCredentials, got %v", err)
		})
	}
}

func TestAliyunCDNProvider_AddDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("Action")
		switch action {
		case "AddCdnDomain":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"RequestId": "req-001"})
		case "DescribeCdnDomainDetail":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"RequestId": "req-002",
				"GetDomainDetailModel": map[string]any{
					"DomainName":   "cdn.example.com",
					"Cname":        "cdn.example.com.w.cdngslb.com",
					"DomainStatus": "configuring",
					"CdnType":      "web",
					"GmtCreated":   "2024-01-01T00:00:00Z",
				},
			})
		default:
			http.Error(w, "unexpected action: "+action, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	p := newAliyunCDNProviderWithClient("kid", "ksecret", srv.URL, srv.Client())
	ctx := context.Background()

	domain, err := p.AddDomain(ctx, AddDomainRequest{
		Domain:       "cdn.example.com",
		BusinessType: BusinessTypeWeb,
		Origins:      []Origin{{Address: "1.2.3.4"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "cdn.example.com", domain.Domain)
	assert.Equal(t, "cdn.example.com.w.cdngslb.com", domain.CNAME)
	assert.Equal(t, DomainStatusConfiguring, domain.Status)
}

func TestAliyunCDNProvider_GetDomain_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"Code":      "InvalidDomain.NotFound",
			"Message":   "The domain name not found.",
			"RequestId": "req-003",
		})
	}))
	defer srv.Close()

	p := newAliyunCDNProviderWithClient("kid", "ksecret", srv.URL, srv.Client())
	_, err := p.GetDomain(context.Background(), "missing.example.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDomainNotFound))
}

func TestAliyunCDNProvider_RemoveDomain(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("Action")
		calls[action]++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"RequestId": "req-004"})
	}))
	defer srv.Close()

	p := newAliyunCDNProviderWithClient("kid", "ksecret", srv.URL, srv.Client())
	err := p.RemoveDomain(context.Background(), "cdn.example.com")
	require.NoError(t, err)
	// Expects both StopCdnDomain and DeleteCdnDomain calls.
	assert.Equal(t, 1, calls["StopCdnDomain"])
	assert.Equal(t, 1, calls["DeleteCdnDomain"])
}

func TestAliyunCDNProvider_ListDomains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"RequestId":  "req-005",
			"TotalCount": 2,
			"PageNumber": 1,
			"PageSize":   500,
			"Domains": map[string]any{
				"PageData": []map[string]any{
					{"DomainName": "a.example.com", "Cname": "a.cdngslb.com", "DomainStatus": "online", "CdnType": "web"},
					{"DomainName": "b.example.com", "Cname": "b.cdngslb.com", "DomainStatus": "offline", "CdnType": "download"},
				},
			},
		})
	}))
	defer srv.Close()

	p := newAliyunCDNProviderWithClient("kid", "ksecret", srv.URL, srv.Client())
	domains, err := p.ListDomains(context.Background())
	require.NoError(t, err)
	require.Len(t, domains, 2)
	assert.Equal(t, DomainStatusOnline, domains[0].Status)
	assert.Equal(t, DomainStatusOffline, domains[1].Status)
	assert.Equal(t, BusinessTypeDownload, domains[1].BusinessType)
}

func TestAliyunCDNProvider_PurgeURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"RequestId":     "req-006",
			"RefreshTaskId": "task-abc",
		})
	}))
	defer srv.Close()

	p := newAliyunCDNProviderWithClient("kid", "ksecret", srv.URL, srv.Client())
	task, err := p.PurgeURLs(context.Background(), []string{"https://cdn.example.com/file.js"})
	require.NoError(t, err)
	assert.Equal(t, "task-abc", task.TaskID)
	assert.Equal(t, TaskStatusProcessing, task.Status)
}

func TestAliyunCDNProvider_PrefetchURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"RequestId":  "req-007",
			"PushTaskId": "push-xyz",
		})
	}))
	defer srv.Close()

	p := newAliyunCDNProviderWithClient("kid", "ksecret", srv.URL, srv.Client())
	task, err := p.PrefetchURLs(context.Background(), []string{"https://cdn.example.com/large.zip"})
	require.NoError(t, err)
	assert.Equal(t, "push-xyz", task.TaskID)
}

func TestAliyunCDNProvider_GetTaskStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"RequestId": "req-008",
			"Tasks": map[string]any{
				"CDNTask": []map[string]any{
					{
						"TaskId":       "task-abc",
						"Status":       "Complete",
						"Process":      "100%",
						"CreationTime": "2024-01-01T00:00:00Z",
					},
				},
			},
		})
	}))
	defer srv.Close()

	p := newAliyunCDNProviderWithClient("kid", "ksecret", srv.URL, srv.Client())
	status, err := p.GetTaskStatus(context.Background(), "task-abc")
	require.NoError(t, err)
	assert.Equal(t, TaskStatusDone, status.Status)
	assert.Equal(t, 100, status.Progress)
	assert.NotNil(t, status.FinishedAt)
}

func TestAliyunCDNProvider_GetTaskStatus_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"RequestId": "req-009",
			"Tasks":     map[string]any{"CDNTask": []any{}},
		})
	}))
	defer srv.Close()

	p := newAliyunCDNProviderWithClient("kid", "ksecret", srv.URL, srv.Client())
	_, err := p.GetTaskStatus(context.Background(), "no-such-task")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTaskNotFound))
}

func TestAliyunCDNProvider_UnsupportedMethods(t *testing.T) {
	p := newAliyunCDNProviderWithClient("k", "s", "http://localhost", &http.Client{})
	ctx := context.Background()

	_, err := p.GetCacheConfig(ctx, "x")
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorIs(t, p.SetCacheConfig(ctx, "x", CacheConfig{}), ErrUnsupported)

	_, err = p.GetBandwidthStats(ctx, "x", StatsRequest{})
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestAliyunCDNProvider_ErrorMapping_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"Code":      "InvalidAccessKeyId.NotFound",
			"Message":   "Specified access key is not found.",
			"RequestId": "req-010",
		})
	}))
	defer srv.Close()

	p := newAliyunCDNProviderWithClient("kid", "ksecret", srv.URL, srv.Client())
	_, err := p.ListDomains(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnauthorized))
}
