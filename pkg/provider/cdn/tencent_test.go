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
var _ Provider = (*tencentCDNProvider)(nil)

func TestNewTencentCDNProvider_MissingCredentials(t *testing.T) {
	tests := []struct {
		name  string
		creds string
	}{
		{"empty JSON", `{}`},
		{"missing secret_key", `{"secret_id":"id"}`},
		{"missing secret_id", `{"secret_key":"key"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewTencentCDNProvider(json.RawMessage(`{}`), json.RawMessage(tt.creds))
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrMissingCredentials))
		})
	}
}

func TestTencentCDNProvider_AddDomain(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		action := r.Header.Get("X-TC-Action")
		w.Header().Set("Content-Type", "application/json")

		switch action {
		case "AddCdnDomain":
			json.NewEncoder(w).Encode(map[string]any{
				"Response": map[string]any{"RequestId": "req-001"},
			})
		case "DescribeDomains":
			json.NewEncoder(w).Encode(map[string]any{
				"Response": map[string]any{
					"RequestId":   "req-002",
					"TotalNumber": 1,
					"Domains": []map[string]any{
						{
							"Domain":      "cdn.example.com",
							"Cname":       "cdn.example.com.cdn.dnsv1.com",
							"Status":      "processing",
							"ServiceType": "web",
							"CreateTime":  "2024-01-01 00:00:00",
						},
					},
				},
			})
		default:
			json.NewEncoder(w).Encode(map[string]any{
				"Response": map[string]any{
					"Error": map[string]string{"Code": "UnknownAction", "Message": "unknown"},
				},
			})
		}
	}))
	defer srv.Close()

	p := newTencentCDNProviderWithClient("sid", "skey", srv.URL, srv.Client())
	domain, err := p.AddDomain(context.Background(), AddDomainRequest{
		Domain:       "cdn.example.com",
		BusinessType: BusinessTypeWeb,
	})
	require.NoError(t, err)
	assert.Equal(t, "cdn.example.com", domain.Domain)
	assert.Equal(t, "cdn.example.com.cdn.dnsv1.com", domain.CNAME)
	assert.Equal(t, DomainStatusConfiguring, domain.Status)
	assert.Equal(t, 2, callCount) // AddCdnDomain + DescribeDomains
}

func TestTencentCDNProvider_GetDomain_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"RequestId":   "req-003",
				"TotalNumber": 0,
				"Domains":     []any{},
			},
		})
	}))
	defer srv.Close()

	p := newTencentCDNProviderWithClient("sid", "skey", srv.URL, srv.Client())
	_, err := p.GetDomain(context.Background(), "missing.example.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDomainNotFound))
}

func TestTencentCDNProvider_RemoveDomain(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("X-TC-Action")
		calls[action]++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{"RequestId": "req-004"},
		})
	}))
	defer srv.Close()

	p := newTencentCDNProviderWithClient("sid", "skey", srv.URL, srv.Client())
	err := p.RemoveDomain(context.Background(), "cdn.example.com")
	require.NoError(t, err)
	assert.Equal(t, 1, calls["StopCdnDomain"])
	assert.Equal(t, 1, calls["DeleteCdnDomain"])
}

func TestTencentCDNProvider_PurgeURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"TaskId":    "purge-task-001",
				"RequestId": "req-005",
			},
		})
	}))
	defer srv.Close()

	p := newTencentCDNProviderWithClient("sid", "skey", srv.URL, srv.Client())
	task, err := p.PurgeURLs(context.Background(), []string{"https://cdn.example.com/a.js"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(task.TaskID, tencentCDNTaskPurgePrefix))
	assert.Equal(t, TaskStatusProcessing, task.Status)
}

func TestTencentCDNProvider_PrefetchURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"TaskId":    "push-task-001",
				"RequestId": "req-006",
			},
		})
	}))
	defer srv.Close()

	p := newTencentCDNProviderWithClient("sid", "skey", srv.URL, srv.Client())
	task, err := p.PrefetchURLs(context.Background(), []string{"https://cdn.example.com/large.bin"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(task.TaskID, tencentCDNTaskPushPrefix))
}

func TestTencentCDNProvider_GetTaskStatus_Purge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("X-TC-Action")
		w.Header().Set("Content-Type", "application/json")
		if action != "DescribePurgeTasks" {
			json.NewEncoder(w).Encode(map[string]any{
				"Response": map[string]any{
					"Error": map[string]string{"Code": "UnknownAction", "Message": "unexpected"},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"RequestId":  "req-007",
				"TotalCount": 1,
				"PurgeLogs": []map[string]any{
					{
						"TaskId":     "purge-task-001",
						"Url":        "https://cdn.example.com/a.js",
						"Status":     "done",
						"CreateTime": "2024-01-01 10:00:00",
						"UpdateTime": "2024-01-01 10:01:00",
					},
				},
			},
		})
	}))
	defer srv.Close()

	p := newTencentCDNProviderWithClient("sid", "skey", srv.URL, srv.Client())
	status, err := p.GetTaskStatus(context.Background(), tencentCDNTaskPurgePrefix+"purge-task-001")
	require.NoError(t, err)
	assert.Equal(t, TaskStatusDone, status.Status)
	assert.Equal(t, 100, status.Progress)
	assert.NotNil(t, status.FinishedAt)
}

func TestTencentCDNProvider_GetTaskStatus_UnknownPrefix(t *testing.T) {
	p := newTencentCDNProviderWithClient("sid", "skey", "http://localhost", &http.Client{})
	_, err := p.GetTaskStatus(context.Background(), "unknown-task-id")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTaskNotFound))
}

func TestTencentCDNProvider_APIError_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"Error": map[string]string{
					"Code":    "AuthFailure.InvalidSecretId",
					"Message": "The SecretId is invalid.",
				},
				"RequestId": "req-008",
			},
		})
	}))
	defer srv.Close()

	p := newTencentCDNProviderWithClient("sid", "skey", srv.URL, srv.Client())
	_, err := p.ListDomains(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnauthorized))
}

func TestTencentCDNProvider_UnsupportedMethods(t *testing.T) {
	p := newTencentCDNProviderWithClient("s", "k", "http://localhost", &http.Client{})
	ctx := context.Background()

	_, err := p.GetCacheConfig(ctx, "x")
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.ErrorIs(t, p.SetCacheConfig(ctx, "x", CacheConfig{}), ErrUnsupported)
	_, err = p.GetHitRateStats(ctx, "x", StatsRequest{})
	assert.ErrorIs(t, err, ErrUnsupported)
}
