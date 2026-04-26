package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func cfProvider(t *testing.T, handler http.Handler) (Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := newCloudflareProviderWithClient("zone-abc", "tok-xyz", srv.URL, srv.Client())
	return p, srv
}

func cfSuccess(v any) []byte {
	b, _ := json.Marshal(map[string]any{"success": true, "errors": nil, "result": v})
	return b
}

func cfError(code int, msg string) []byte {
	b, _ := json.Marshal(map[string]any{
		"success": false,
		"errors":  []map[string]any{{"code": code, "message": msg}},
		"result":  nil,
	})
	return b
}

func cfRecordJSON(id, typ, name, content string, ttl int) map[string]any {
	return map[string]any{
		"id": id, "type": typ, "name": name,
		"content": content, "ttl": ttl, "proxied": false,
	}
}

// ── ListRecords ───────────────────────────────────────────────────────────────

func TestCloudflare_ListRecords_HappyPath(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/zones/zone-abc/dns_records")
		assert.Equal(t, "Bearer tok-xyz", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess([]any{
			cfRecordJSON("r1", "A", "example.com", "1.2.3.4", 300),
			cfRecordJSON("r2", "CNAME", "www.example.com", "example.com", 1),
		}))
	}))

	records, err := p.ListRecords(context.Background(), "zone-abc", RecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "r1", records[0].ID)
	assert.Equal(t, "A", records[0].Type)
	assert.Equal(t, "1.2.3.4", records[0].Content)
	assert.Equal(t, "r2", records[1].ID)
	assert.Equal(t, "CNAME", records[1].Type)
}

func TestCloudflare_ListRecords_Empty(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess([]any{}))
	}))

	records, err := p.ListRecords(context.Background(), "", RecordFilter{})
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestCloudflare_ListRecords_FilterPassedToURL(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "A", r.URL.Query().Get("type"))
		assert.Equal(t, "api.example.com", r.URL.Query().Get("name"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess([]any{}))
	}))

	_, err := p.ListRecords(context.Background(), "zone-abc", RecordFilter{Type: "A", Name: "api.example.com"})
	require.NoError(t, err)
}

func TestCloudflare_ListRecords_UsesProviderZoneWhenEmpty(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/zones/zone-abc/")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess([]any{}))
	}))

	// pass empty zone → should fall back to provider's zone "zone-abc"
	_, err := p.ListRecords(context.Background(), "", RecordFilter{})
	require.NoError(t, err)
}

func TestCloudflare_ListRecords_Unauthorized(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(cfError(10000, "Invalid credentials"))
	}))

	_, err := p.ListRecords(context.Background(), "zone-abc", RecordFilter{})
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestCloudflare_ListRecords_RateLimit(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"success":false}`))
	}))

	_, err := p.ListRecords(context.Background(), "zone-abc", RecordFilter{})
	assert.ErrorIs(t, err, ErrRateLimitExceeded)
}

func TestCloudflare_ListRecords_APIReturnsFalseSuccess(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfError(1004, "Zone not found"))
	}))

	_, err := p.ListRecords(context.Background(), "zone-abc", RecordFilter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Zone not found")
}

// ── MX record priority ────────────────────────────────────────────────────────

func TestCloudflare_ListRecords_MXPriority(t *testing.T) {
	prio := 10
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := map[string]any{
			"id": "mx1", "type": "MX", "name": "example.com",
			"content": "mail.example.com", "ttl": 300,
			"priority": prio, "proxied": false,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess([]any{rec}))
	}))

	records, err := p.ListRecords(context.Background(), "zone-abc", RecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, 10, records[0].Priority)
}

// ── CreateRecord ──────────────────────────────────────────────────────────────

func TestCloudflare_CreateRecord_HappyPath(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		var body cloudflareCreateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "A", body.Type)
		assert.Equal(t, "api.example.com", body.Name)
		assert.Equal(t, "5.6.7.8", body.Content)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cfSuccess(cfRecordJSON("new-id", "A", "api.example.com", "5.6.7.8", 300)))
	}))

	rec, err := p.CreateRecord(context.Background(), "zone-abc", Record{
		Type: "A", Name: "api.example.com", Content: "5.6.7.8", TTL: 300,
	})
	require.NoError(t, err)
	assert.Equal(t, "new-id", rec.ID)
}

func TestCloudflare_CreateRecord_Forbidden(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(cfError(10000, "Forbidden"))
	}))

	_, err := p.CreateRecord(context.Background(), "zone-abc", Record{Type: "A", Name: "x", Content: "1.2.3.4", TTL: 1})
	assert.ErrorIs(t, err, ErrUnauthorized)
}

// ── UpdateRecord ──────────────────────────────────────────────────────────────

func TestCloudflare_UpdateRecord_HappyPath(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, "/dns_records/rec-id-1")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess(cfRecordJSON("rec-id-1", "A", "example.com", "9.9.9.9", 300)))
	}))

	rec, err := p.UpdateRecord(context.Background(), "zone-abc", "rec-id-1", Record{
		Type: "A", Name: "example.com", Content: "9.9.9.9", TTL: 300,
	})
	require.NoError(t, err)
	assert.Equal(t, "rec-id-1", rec.ID)
	assert.Equal(t, "9.9.9.9", rec.Content)
}

func TestCloudflare_UpdateRecord_NotFound(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write(cfError(1032, "Record not found"))
	}))

	_, err := p.UpdateRecord(context.Background(), "zone-abc", "ghost-id", Record{Type: "A", Name: "x", Content: "1.1.1.1"})
	assert.ErrorIs(t, err, ErrRecordNotFound)
}

// ── DeleteRecord ──────────────────────────────────────────────────────────────

func TestCloudflare_DeleteRecord_HappyPath(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/dns_records/del-id")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess(map[string]any{"id": "del-id"}))
	}))

	err := p.DeleteRecord(context.Background(), "zone-abc", "del-id")
	assert.NoError(t, err)
}

func TestCloudflare_DeleteRecord_NotFound(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write(cfError(1032, "Record not found"))
	}))

	err := p.DeleteRecord(context.Background(), "zone-abc", "ghost")
	assert.ErrorIs(t, err, ErrRecordNotFound)
}

// ── GetNameservers ────────────────────────────────────────────────────────────

func TestCloudflare_GetNameservers_HappyPath(t *testing.T) {
	ns := []string{"ns1.cloudflare.com", "ns2.cloudflare.com"}
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/zones/zone-abc", r.URL.Path)

		result := map[string]any{"name_servers": ns}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess(result))
	}))

	got, err := p.GetNameservers(context.Background(), "zone-abc")
	require.NoError(t, err)
	assert.Equal(t, ns, got)
}

func TestCloudflare_GetNameservers_FallbackToProviderZone(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/zones/zone-abc", r.URL.Path) // provider's own zone
		result := map[string]any{"name_servers": []string{"ns1.cf.com"}}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess(result))
	}))

	_, err := p.GetNameservers(context.Background(), "")
	require.NoError(t, err)
}

func TestCloudflare_GetNameservers_ZoneNotFound(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write(cfError(1001, "Zone not found"))
	}))

	_, err := p.GetNameservers(context.Background(), "zone-abc")
	// 404 from cfCheckStatus maps to ErrRecordNotFound; wrapped inside GetNameservers
	assert.ErrorIs(t, err, ErrRecordNotFound)
}

func TestCloudflare_GetNameservers_EmptyList(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := map[string]any{"name_servers": []string{}}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess(result))
	}))

	_, err := p.GetNameservers(context.Background(), "zone-abc")
	assert.ErrorIs(t, err, ErrZoneNotFound)
}

// ── BatchCreateRecords ────────────────────────────────────────────────────────

func TestCloudflare_BatchCreateRecords_HappyPath(t *testing.T) {
	call := 0
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		id := fmt.Sprintf("created-%d", call)
		var body cloudflareCreateRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess(cfRecordJSON(id, body.Type, body.Name, body.Content, body.TTL)))
	}))

	in := []Record{
		{Type: "A", Name: "a.example.com", Content: "1.1.1.1", TTL: 300},
		{Type: "A", Name: "b.example.com", Content: "2.2.2.2", TTL: 300},
		{Type: "CNAME", Name: "c.example.com", Content: "a.example.com", TTL: 1},
	}
	out, err := p.BatchCreateRecords(context.Background(), "zone-abc", in)
	require.NoError(t, err)
	assert.Len(t, out, 3)
	assert.Equal(t, "created-1", out[0].ID)
	assert.Equal(t, "created-2", out[1].ID)
	assert.Equal(t, "created-3", out[2].ID)
}

func TestCloudflare_BatchCreateRecords_PartialFailure(t *testing.T) {
	call := 0
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 2 {
			// second record fails with rate limit
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		var body cloudflareCreateRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess(cfRecordJSON(fmt.Sprintf("id-%d", call), body.Type, body.Name, body.Content, body.TTL)))
	}))

	in := []Record{
		{Type: "A", Name: "ok.example.com", Content: "1.1.1.1", TTL: 300},
		{Type: "A", Name: "fail.example.com", Content: "2.2.2.2", TTL: 300},
		{Type: "A", Name: "never.example.com", Content: "3.3.3.3", TTL: 300},
	}
	out, err := p.BatchCreateRecords(context.Background(), "zone-abc", in)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateLimitExceeded)
	// only the first record was created before the failure
	assert.Len(t, out, 1)
	assert.Equal(t, "id-1", out[0].ID)
}

func TestCloudflare_BatchCreateRecords_EmptySlice(t *testing.T) {
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call API for empty slice")
	}))

	out, err := p.BatchCreateRecords(context.Background(), "zone-abc", []Record{})
	require.NoError(t, err)
	assert.Empty(t, out)
}

// ── BatchDeleteRecords ────────────────────────────────────────────────────────

func TestCloudflare_BatchDeleteRecords_HappyPath(t *testing.T) {
	deleted := []string{}
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		// extract record ID from path /.../dns_records/{id}
		parts := splitPath(r.URL.Path)
		deleted = append(deleted, parts[len(parts)-1])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess(map[string]any{"id": parts[len(parts)-1]}))
	}))

	err := p.BatchDeleteRecords(context.Background(), "zone-abc", []string{"id-1", "id-2", "id-3"})
	require.NoError(t, err)
	assert.Equal(t, []string{"id-1", "id-2", "id-3"}, deleted)
}

func TestCloudflare_BatchDeleteRecords_StopsOnFirstError(t *testing.T) {
	call := 0
	p, _ := cfProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 2 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(cfError(1032, "record not found"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfSuccess(map[string]any{"id": "x"}))
	}))

	err := p.BatchDeleteRecords(context.Background(), "zone-abc", []string{"ok-1", "missing", "never"})
	assert.ErrorIs(t, err, ErrRecordNotFound)
	// only 2 calls: first succeeds, second fails, third never runs
	assert.Equal(t, 2, call)
}

// ── cfCheckStatus ─────────────────────────────────────────────────────────────

func TestCfCheckStatus(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		body    []byte
		wantErr error
		wantMsg string
	}{
		{"200 ok", http.StatusOK, nil, nil, ""},
		{"201 created", http.StatusCreated, nil, nil, ""},
		{"204 no content", http.StatusNoContent, nil, nil, ""},
		{"401 unauthorized", http.StatusUnauthorized, cfError(10000, "bad token"), ErrUnauthorized, ""},
		{"403 forbidden", http.StatusForbidden, cfError(10000, "forbidden"), ErrUnauthorized, ""},
		{"404 not found", http.StatusNotFound, cfError(1032, "record not found"), ErrRecordNotFound, ""},
		{"429 rate limit", http.StatusTooManyRequests, []byte(`{}`), ErrRateLimitExceeded, ""},
		{"500 with cf error", http.StatusInternalServerError, cfError(1000, "server error"), nil, "cloudflare error 1000: server error"},
		{"500 raw body", http.StatusInternalServerError, []byte("internal server error"), nil, "cloudflare HTTP 500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cfCheckStatus(tt.code, tt.body)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else if tt.wantMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCfCheckStatus_LongBodyTruncated(t *testing.T) {
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'x'
	}
	err := cfCheckStatus(http.StatusInternalServerError, long)
	require.Error(t, err)
	// error message should be truncated at 200 chars + ellipsis
	assert.LessOrEqual(t, len(err.Error()), 300)
}

// ── Name() ────────────────────────────────────────────────────────────────────

func TestCloudflare_Name(t *testing.T) {
	p, _ := cfProvider(t, http.NewServeMux())
	assert.Equal(t, "cloudflare", p.Name())
}

// ── helpers ───────────────────────────────────────────────────────────────────

// splitPath splits a URL path into parts, filtering empty strings.
func splitPath(path string) []string {
	parts := []string{}
	cur := ""
	for _, c := range path {
		if c == '/' {
			if cur != "" {
				parts = append(parts, cur)
				cur = ""
			}
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		parts = append(parts, cur)
	}
	return parts
}
