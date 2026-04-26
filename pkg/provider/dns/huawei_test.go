package dns

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Test helpers ──────────────────────────────────────────────────────────────

const (
	hwTestZoneID = "zone-huawei-12345"
	hwTestDomain = "example.com"
)

// newHuaweiTestProvider builds a provider wired to the given handler.
// The handler must respond to both zone-lookup calls and recordset calls.
func newHuaweiTestProvider(t *testing.T, handler http.HandlerFunc) *hwProvider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := newHuaweiDNSProviderWithClient(hwTestDomain, "AK", "SK", srv.URL, srv.Client())
	return p.(*hwProvider)
}

// hwZoneLookupThenHandler returns a handler that answers the first zone-lookup
// GET request and delegates all subsequent requests to inner.
func hwZoneLookupThenHandler(t *testing.T, inner http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/zones") && !strings.Contains(r.URL.Path, "/recordsets") && !strings.Contains(r.URL.Path, "/nameservers") {
			// Zone lookup
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"zones": []map[string]any{
					{"id": hwTestZoneID, "name": hwTestDomain + "."},
				},
			})
			return
		}
		inner(w, r)
	}
}

// ── Constructor ───────────────────────────────────────────────────────────────

func TestNewHuaweiDNSProvider_Valid(t *testing.T) {
	cfg := json.RawMessage(`{"domain_name":"example.com"}`)
	creds := json.RawMessage(`{"access_key":"AKID","secret_key":"skey"}`)
	p, err := NewHuaweiDNSProvider(cfg, creds)
	require.NoError(t, err)
	assert.Equal(t, "huaweidns", p.Name())
}

func TestNewHuaweiDNSProvider_MissingDomainName(t *testing.T) {
	cfg := json.RawMessage(`{"domain_name":""}`)
	creds := json.RawMessage(`{"access_key":"AKID","secret_key":"skey"}`)
	_, err := NewHuaweiDNSProvider(cfg, creds)
	require.ErrorIs(t, err, ErrMissingConfig)
}

func TestNewHuaweiDNSProvider_MissingAccessKey(t *testing.T) {
	cfg := json.RawMessage(`{"domain_name":"example.com"}`)
	creds := json.RawMessage(`{"access_key":"","secret_key":"skey"}`)
	_, err := NewHuaweiDNSProvider(cfg, creds)
	require.ErrorIs(t, err, ErrMissingCredentials)
}

func TestNewHuaweiDNSProvider_MissingSecretKey(t *testing.T) {
	cfg := json.RawMessage(`{"domain_name":"example.com"}`)
	creds := json.RawMessage(`{"access_key":"AKID","secret_key":""}`)
	_, err := NewHuaweiDNSProvider(cfg, creds)
	require.ErrorIs(t, err, ErrMissingCredentials)
}

func TestNewHuaweiDNSProvider_InvalidConfigJSON(t *testing.T) {
	_, err := NewHuaweiDNSProvider(json.RawMessage(`{bad}`), json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrMissingConfig)
}

// ── Name ──────────────────────────────────────────────────────────────────────

func TestHuawei_Name(t *testing.T) {
	p := newHuaweiTestProvider(t, func(w http.ResponseWriter, r *http.Request) {})
	assert.Equal(t, "huaweidns", p.Name())
}

// ── Zone lookup + cache ───────────────────────────────────────────────────────

func TestHuawei_ZoneLookup_ReturnsZoneID(t *testing.T) {
	lookupCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/zones") && !strings.Contains(r.URL.Path, "recordsets") {
			lookupCount++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"zones": []map[string]any{
					{"id": hwTestZoneID, "name": "example.com."},
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"recordsets": []any{},
		})
	}

	p := newHuaweiTestProvider(t, handler)
	// Two calls should only trigger one zone lookup (cache)
	_, _ = p.ListRecords(t.Context(), "", RecordFilter{})
	_, _ = p.ListRecords(t.Context(), "", RecordFilter{})
	assert.Equal(t, 1, lookupCount, "zone should be cached after first lookup")
}

func TestHuawei_ZoneLookup_NotFound(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"zones": []any{}, // empty — zone not found
		})
	}

	p := newHuaweiTestProvider(t, handler)
	_, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.ErrorIs(t, err, ErrZoneNotFound)
}

func TestHuawei_ZoneLookup_UsesExplicitZone(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/zones") && !strings.Contains(r.URL.Path, "recordsets") {
			assert.Contains(t, r.URL.RawQuery, "name=other.com")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"zones": []map[string]any{
					{"id": "zone-other", "name": "other.com."},
				},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"recordsets": []any{}})
	}

	p := newHuaweiTestProvider(t, handler)
	_, err := p.ListRecords(t.Context(), "other.com", RecordFilter{})
	require.NoError(t, err)
}

// ── ListRecords ───────────────────────────────────────────────────────────────

func TestHuawei_ListRecords_HappyPath(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, hwTestZoneID+"/recordsets")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"recordsets": []map[string]any{
				{
					"id":      "rs-001",
					"name":    "www.example.com.",
					"type":    "A",
					"records": []string{"1.2.3.4"},
					"ttl":     300,
				},
				{
					"id":      "rs-002",
					"name":    "example.com.",
					"type":    "MX",
					"records": []string{"10 mail.example.com."},
					"ttl":     600,
				},
			},
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	records, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 2)

	assert.Equal(t, "rs-001", records[0].ID)
	assert.Equal(t, "A", records[0].Type)
	assert.Equal(t, "www.example.com", records[0].Name)
	assert.Equal(t, "1.2.3.4", records[0].Content)
	assert.Equal(t, 300, records[0].TTL)

	assert.Equal(t, "rs-002", records[1].ID)
	assert.Equal(t, "MX", records[1].Type)
	assert.Equal(t, "example.com", records[1].Name)
	assert.Equal(t, 10, records[1].Priority)
	assert.Equal(t, "mail.example.com", records[1].Content) // trailing dot stripped
}

func TestHuawei_ListRecords_MultiValueRecordset(t *testing.T) {
	// One recordset with two A values → two provider Records with same ID
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"recordsets": []map[string]any{
				{
					"id":      "rs-multi",
					"name":    "api.example.com.",
					"type":    "A",
					"records": []string{"1.1.1.1", "2.2.2.2"},
					"ttl":     60,
				},
			},
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	records, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "rs-multi", records[0].ID)
	assert.Equal(t, "rs-multi", records[1].ID)
	assert.Equal(t, "1.1.1.1", records[0].Content)
	assert.Equal(t, "2.2.2.2", records[1].Content)
}

func TestHuawei_ListRecords_FilterType(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, "type=AAAA")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"recordsets": []any{}})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	records, err := p.ListRecords(t.Context(), "", RecordFilter{Type: "AAAA"})
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestHuawei_ListRecords_FilterName(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, "name=api.example.com.")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"recordsets": []any{}})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	records, err := p.ListRecords(t.Context(), "", RecordFilter{Name: "api.example.com"})
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestHuawei_ListRecords_Pagination(t *testing.T) {
	callCount := 0
	inner := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			// First page — include Next link
			recs := make([]map[string]any, hwPageLimit)
			for i := 0; i < hwPageLimit; i++ {
				recs[i] = map[string]any{
					"id":      fmt.Sprintf("rs-%04d", i),
					"name":    fmt.Sprintf("sub%d.example.com.", i),
					"type":    "A",
					"records": []string{"1.1.1.1"},
					"ttl":     300,
				}
			}
			json.NewEncoder(w).Encode(map[string]any{
				"recordsets": recs,
				"links":      map[string]any{"self": "...", "next": "..."},
			})
		} else {
			// Second page — no Next link
			json.NewEncoder(w).Encode(map[string]any{
				"recordsets": []map[string]any{
					{
						"id":      "rs-last",
						"name":    "last.example.com.",
						"type":    "A",
						"records": []string{"9.9.9.9"},
						"ttl":     300,
					},
				},
			})
		}
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	records, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.NoError(t, err)
	assert.Len(t, records, hwPageLimit+1)
	assert.Equal(t, 2, callCount)
}

func TestHuawei_ListRecords_Empty(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"recordsets": []any{}})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	records, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.NoError(t, err)
	assert.Empty(t, records)
}

// ── CreateRecord ──────────────────────────────────────────────────────────────

func TestHuawei_CreateRecord_HappyPath(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, hwTestZoneID+"/recordsets")

		var body hwRecordsetRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "www.example.com.", body.Name)
		assert.Equal(t, "A", body.Type)
		assert.Equal(t, []string{"1.2.3.4"}, body.Records)
		assert.Equal(t, 300, body.TTL)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(hwRecordset{
			ID:      "rs-new-001",
			Name:    "www.example.com.",
			Type:    "A",
			Records: []string{"1.2.3.4"},
			TTL:     300,
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	created, err := p.CreateRecord(t.Context(), "", Record{
		Type: "A", Name: "www.example.com", Content: "1.2.3.4", TTL: 300,
	})
	require.NoError(t, err)
	assert.Equal(t, "rs-new-001", created.ID)
	assert.Equal(t, "www.example.com", created.Name)
	assert.Equal(t, "1.2.3.4", created.Content)
}

func TestHuawei_CreateRecord_MXPriority(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		var body hwRecordsetRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "MX", body.Type)
		// MX value must embed priority: "10 mail.example.com."
		require.Len(t, body.Records, 1)
		assert.Equal(t, "10 mail.example.com.", body.Records[0])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hwRecordset{
			ID:      "rs-mx",
			Name:    "example.com.",
			Type:    "MX",
			Records: []string{"10 mail.example.com."},
			TTL:     600,
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	created, err := p.CreateRecord(t.Context(), "", Record{
		Type: "MX", Name: "example.com", Content: "mail.example.com", TTL: 600, Priority: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, 10, created.Priority)
	assert.Equal(t, "mail.example.com", created.Content)
}

func TestHuawei_CreateRecord_CNAMEGetsTrailingDot(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		var body hwRecordsetRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Len(t, body.Records, 1)
		// CNAME target must end with dot
		assert.True(t, strings.HasSuffix(body.Records[0], "."), "CNAME target should have trailing dot")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hwRecordset{
			ID:      "rs-cname",
			Name:    "www.example.com.",
			Type:    "CNAME",
			Records: []string{"backend.example.com."},
			TTL:     300,
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	_, err := p.CreateRecord(t.Context(), "", Record{
		Type: "CNAME", Name: "www.example.com", Content: "backend.example.com", TTL: 300,
	})
	require.NoError(t, err)
}

func TestHuawei_CreateRecord_AlreadyExists(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"code":    "DNS.0417",
			"message": "recordset already exists",
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	_, err := p.CreateRecord(t.Context(), "", Record{Type: "A", Name: "dup.example.com", Content: "1.1.1.1", TTL: 300})
	require.ErrorIs(t, err, ErrRecordAlreadyExists)
}

// ── UpdateRecord ──────────────────────────────────────────────────────────────

func TestHuawei_UpdateRecord_HappyPath(t *testing.T) {
	const recordID = "rs-update-001"
	inner := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, recordID)

		var body hwRecordsetRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "A", body.Type)
		assert.Equal(t, []string{"5.6.7.8"}, body.Records)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hwRecordset{
			ID:      recordID,
			Name:    "api.example.com.",
			Type:    "A",
			Records: []string{"5.6.7.8"},
			TTL:     300,
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	updated, err := p.UpdateRecord(t.Context(), "", recordID, Record{
		Type: "A", Name: "api.example.com", Content: "5.6.7.8", TTL: 300,
	})
	require.NoError(t, err)
	assert.Equal(t, recordID, updated.ID)
	assert.Equal(t, "5.6.7.8", updated.Content)
}

func TestHuawei_UpdateRecord_NotFound(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"code":    "DNS.0601",
			"message": "recordset not found",
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	_, err := p.UpdateRecord(t.Context(), "", "does-not-exist", Record{
		Type: "A", Name: "test.example.com", Content: "1.1.1.1", TTL: 300,
	})
	require.ErrorIs(t, err, ErrRecordNotFound)
}

// ── DeleteRecord ──────────────────────────────────────────────────────────────

func TestHuawei_DeleteRecord_HappyPath(t *testing.T) {
	const recordID = "rs-del-001"
	inner := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, recordID)
		w.WriteHeader(http.StatusNoContent)
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	err := p.DeleteRecord(t.Context(), "", recordID)
	require.NoError(t, err)
}

func TestHuawei_DeleteRecord_NotFound(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"code": "DNS.0601", "message": "not found",
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	err := p.DeleteRecord(t.Context(), "", "gone")
	require.ErrorIs(t, err, ErrRecordNotFound)
}

func TestHuawei_DeleteRecord_Unauthorized(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	err := p.DeleteRecord(t.Context(), "", "rs-abc")
	require.ErrorIs(t, err, ErrUnauthorized)
}

// ── GetNameservers ────────────────────────────────────────────────────────────

func TestHuawei_GetNameservers_HappyPath(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/nameservers")
		assert.Contains(t, r.URL.RawQuery, hwTestZoneID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"nameservers": []map[string]any{
				{"hostname": "ns1.huaweicloud-dns.com.", "priority": 1},
				{"hostname": "ns2.huaweicloud-dns.net.", "priority": 2},
			},
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	ns, err := p.GetNameservers(t.Context(), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"ns1.huaweicloud-dns.com", "ns2.huaweicloud-dns.net"}, ns)
}

func TestHuawei_GetNameservers_Empty(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"nameservers": []any{},
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	_, err := p.GetNameservers(t.Context(), "")
	require.ErrorIs(t, err, ErrZoneNotFound)
}

// ── BatchCreateRecords ────────────────────────────────────────────────────────

func TestHuawei_BatchCreateRecords_HappyPath(t *testing.T) {
	callCount := 0
	inner := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hwRecordset{
			ID:      fmt.Sprintf("rs-batch-%d", callCount),
			Name:    "test.example.com.",
			Type:    "A",
			Records: []string{"1.1.1.1"},
			TTL:     300,
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	records := []Record{
		{Type: "A", Name: "a.example.com", Content: "1.1.1.1", TTL: 300},
		{Type: "A", Name: "b.example.com", Content: "2.2.2.2", TTL: 300},
	}
	created, err := p.BatchCreateRecords(t.Context(), "", records)
	require.NoError(t, err)
	assert.Len(t, created, 2)
	assert.Equal(t, 2, callCount)
}

func TestHuawei_BatchCreateRecords_StopsOnError(t *testing.T) {
	callCount := 0
	inner := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		callCount++
		if callCount == 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{"code": "DNS.0417", "message": "already exists"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hwRecordset{
			ID:      fmt.Sprintf("rs-%d", callCount),
			Name:    "ok.example.com.",
			Type:    "A",
			Records: []string{"1.1.1.1"},
			TTL:     300,
		})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	records := []Record{
		{Type: "A", Name: "ok.example.com", Content: "1.1.1.1", TTL: 300},
		{Type: "A", Name: "dup.example.com", Content: "2.2.2.2", TTL: 300},
		{Type: "A", Name: "skip.example.com", Content: "3.3.3.3", TTL: 300},
	}
	created, err := p.BatchCreateRecords(t.Context(), "", records)
	require.ErrorIs(t, err, ErrRecordAlreadyExists)
	assert.Len(t, created, 1)
	assert.Equal(t, 2, callCount)
}

// ── BatchDeleteRecords ────────────────────────────────────────────────────────

func TestHuawei_BatchDeleteRecords_HappyPath(t *testing.T) {
	callCount := 0
	inner := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			return
		}
		callCount++
		w.WriteHeader(http.StatusNoContent)
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	err := p.BatchDeleteRecords(t.Context(), "", []string{"id-1", "id-2", "id-3"})
	require.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

// ── hwCheckHTTP table-driven ──────────────────────────────────────────────────

func TestHwCheckHTTP(t *testing.T) {
	cases := []struct {
		name    string
		code    int
		body    []byte
		wantNil bool
		wantErr error
	}{
		{"200 ok", 200, nil, true, nil},
		{"202 accepted", 202, nil, true, nil},
		{"204 no content", 204, nil, true, nil},
		{"401 unauthorized (no body)", 401, nil, false, ErrUnauthorized},
		{"403 forbidden (no body)", 403, nil, false, ErrUnauthorized},
		{"404 not found (no body)", 404, nil, false, ErrRecordNotFound},
		{"409 conflict (no body)", 409, nil, false, ErrRecordAlreadyExists},
		{"429 rate limit", 429, nil, false, ErrRateLimitExceeded},
		{"500 server error", 500, []byte("internal error"), false, nil},
		// API error codes in body
		{"DNS.0601 record not found", 404, []byte(`{"code":"DNS.0601","message":"not found"}`), false, ErrRecordNotFound},
		{"DNS.0401 zone not found", 404, []byte(`{"code":"DNS.0401","message":"zone not found"}`), false, ErrZoneNotFound},
		{"DNS.0417 already exists", 409, []byte(`{"code":"DNS.0417","message":"already exists"}`), false, ErrRecordAlreadyExists},
		{"DNS.0101 unauthorized", 401, []byte(`{"code":"DNS.0101","message":"unauthorized"}`), false, ErrUnauthorized},
		{"DNS.0501 rate limit", 429, []byte(`{"code":"DNS.0501","message":"rate limit"}`), false, ErrRateLimitExceeded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := hwCheckHTTP(tc.code, tc.body)
			if tc.wantNil {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

// ── hwMapCode table-driven ────────────────────────────────────────────────────

func TestHwMapCode(t *testing.T) {
	cases := []struct {
		code    string
		wantErr error
	}{
		{"DNS.0101", ErrUnauthorized},
		{"DNS.0103", ErrUnauthorized},
		{"DNS.0112", ErrUnauthorized},
		{"APIG.0301", ErrUnauthorized},
		{"DNS.0601", ErrRecordNotFound},
		{"DNS.0602", ErrRecordNotFound},
		{"DNS.0603", ErrRecordNotFound},
		{"DNS.0401", ErrZoneNotFound},
		{"DNS.0403", ErrZoneNotFound},
		{"DNS.0501", ErrRateLimitExceeded},
		{"DNS.0502", ErrRateLimitExceeded},
		{"DNS.0417", ErrRecordAlreadyExists},
		{"DNS.9999", nil}, // unknown → generic error
	}

	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			err := hwMapCode(tc.code, "test msg")
			require.Error(t, err)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			} else {
				assert.Contains(t, err.Error(), tc.code)
			}
		})
	}
}

// ── hwRecordsetToRecords ──────────────────────────────────────────────────────

func TestHwRecordsetToRecords_StripsTrailingDot(t *testing.T) {
	rs := hwRecordset{
		ID:      "rs-dot",
		Name:    "www.example.com.",
		Type:    "A",
		Records: []string{"9.9.9.9"},
		TTL:     300,
	}
	records := hwRecordsetToRecords(rs)
	require.Len(t, records, 1)
	assert.Equal(t, "www.example.com", records[0].Name)
}

func TestHwRecordsetToRecords_MXParsesPriority(t *testing.T) {
	rs := hwRecordset{
		ID:      "rs-mx",
		Name:    "example.com.",
		Type:    "MX",
		Records: []string{"20 backup-mail.example.com."},
		TTL:     600,
	}
	records := hwRecordsetToRecords(rs)
	require.Len(t, records, 1)
	assert.Equal(t, 20, records[0].Priority)
	assert.Equal(t, "backup-mail.example.com", records[0].Content)
}

func TestHwRecordsetToRecords_EmptyRecords(t *testing.T) {
	rs := hwRecordset{ID: "rs-empty", Name: "x.example.com.", Type: "A", Records: []string{}}
	records := hwRecordsetToRecords(rs)
	assert.Empty(t, records)
}

// ── hwFQDN / hwRecordName ─────────────────────────────────────────────────────

func TestHwFQDN(t *testing.T) {
	assert.Equal(t, "example.com.", hwFQDN("example.com"))
	assert.Equal(t, "example.com.", hwFQDN("example.com.")) // idempotent
}

func TestHwRecordName(t *testing.T) {
	assert.Equal(t, "example.com", hwRecordName("example.com."))
	assert.Equal(t, "example.com", hwRecordName("example.com")) // no-op
}

// ── hwRecordValue ─────────────────────────────────────────────────────────────

func TestHwRecordValue_A(t *testing.T) {
	assert.Equal(t, "1.2.3.4", hwRecordValue(Record{Type: "A", Content: "1.2.3.4"}))
}

func TestHwRecordValue_CNAME(t *testing.T) {
	assert.Equal(t, "target.example.com.", hwRecordValue(Record{Type: "CNAME", Content: "target.example.com"}))
}

func TestHwRecordValue_NS(t *testing.T) {
	assert.Equal(t, "ns1.example.com.", hwRecordValue(Record{Type: "NS", Content: "ns1.example.com"}))
}

func TestHwRecordValue_MX(t *testing.T) {
	assert.Equal(t, "10 mail.example.com.", hwRecordValue(Record{Type: "MX", Content: "mail.example.com", Priority: 10}))
}

func TestHwRecordValue_TXT(t *testing.T) {
	assert.Equal(t, "v=spf1 include:example.com ~all", hwRecordValue(Record{Type: "TXT", Content: "v=spf1 include:example.com ~all"}))
}

// ── Registry ──────────────────────────────────────────────────────────────────

func TestRegistry_HuaweiDNSRegistered(t *testing.T) {
	cfg := json.RawMessage(`{"domain_name":"example.com"}`)
	creds := json.RawMessage(`{"access_key":"AK","secret_key":"SK"}`)
	p, err := Get("huaweidns", cfg, creds)
	require.NoError(t, err)
	assert.Equal(t, "huaweidns", p.Name())
}

// ── Request headers ───────────────────────────────────────────────────────────

func TestHuawei_RequestContainsRequiredHeaders(t *testing.T) {
	var capturedHeaders http.Header
	inner := func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"recordsets": []any{}})
	}

	p := newHuaweiTestProvider(t, hwZoneLookupThenHandler(t, inner))
	_, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.NoError(t, err)

	assert.NotEmpty(t, capturedHeaders.Get("Authorization"))
	assert.Equal(t, "application/json", capturedHeaders.Get("Content-Type"))
	assert.NotEmpty(t, capturedHeaders.Get("X-Sdk-Date"))
	assert.True(t,
		strings.HasPrefix(capturedHeaders.Get("Authorization"), "SDK-HMAC-SHA256 "),
		"should use SDK-HMAC-SHA256 scheme",
	)
}
