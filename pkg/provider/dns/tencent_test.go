package dns

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Test helpers ──────────────────────────────────────────────────────────────

func newDNSPodTestProvider(t *testing.T, handler http.HandlerFunc) *dnspodProvider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := newDNSPodProviderWithClient("example.com", "ID", "Key", srv.URL, srv.Client())
	return p.(*dnspodProvider)
}

// dnspodSuccessResponse wraps extra fields into a valid dnspod envelope.
func dnspodSuccessResponse(t *testing.T, extra map[string]any) []byte {
	t.Helper()
	resp := map[string]any{
		"Response": extra,
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	return b
}

// dnspodErrorResponse returns a valid error envelope.
func dnspodErrorResponse(t *testing.T, code, message string) []byte {
	t.Helper()
	return dnspodSuccessResponse(t, map[string]any{
		"RequestId": "req-err",
		"Error":     map[string]any{"Code": code, "Message": message},
	})
}

// readActionFromRequest parses the X-TC-Action header from a request.
func readActionFromRequest(r *http.Request) string {
	return r.Header.Get("X-TC-Action")
}

// readBodyAsMap reads and decodes the JSON body into a map.
func readBodyAsMap(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

// ── Constructor ───────────────────────────────────────────────────────────────

func TestNewDNSPodProvider_Valid(t *testing.T) {
	cfg := json.RawMessage(`{"domain_name":"example.com"}`)
	creds := json.RawMessage(`{"secret_id":"AKIDtest","secret_key":"keysecret"}`)
	p, err := NewDNSPodProvider(cfg, creds)
	require.NoError(t, err)
	assert.Equal(t, "dnspod", p.Name())
}

func TestNewDNSPodProvider_MissingDomainName(t *testing.T) {
	cfg := json.RawMessage(`{"domain_name":""}`)
	creds := json.RawMessage(`{"secret_id":"AKIDtest","secret_key":"keysecret"}`)
	_, err := NewDNSPodProvider(cfg, creds)
	require.ErrorIs(t, err, ErrMissingConfig)
}

func TestNewDNSPodProvider_MissingSecretID(t *testing.T) {
	cfg := json.RawMessage(`{"domain_name":"example.com"}`)
	creds := json.RawMessage(`{"secret_id":"","secret_key":"keysecret"}`)
	_, err := NewDNSPodProvider(cfg, creds)
	require.ErrorIs(t, err, ErrMissingCredentials)
}

func TestNewDNSPodProvider_MissingSecretKey(t *testing.T) {
	cfg := json.RawMessage(`{"domain_name":"example.com"}`)
	creds := json.RawMessage(`{"secret_id":"AKIDtest","secret_key":""}`)
	_, err := NewDNSPodProvider(cfg, creds)
	require.ErrorIs(t, err, ErrMissingCredentials)
}

func TestNewDNSPodProvider_InvalidConfigJSON(t *testing.T) {
	cfg := json.RawMessage(`not-json`)
	creds := json.RawMessage(`{"secret_id":"AKIDtest","secret_key":"key"}`)
	_, err := NewDNSPodProvider(cfg, creds)
	require.ErrorIs(t, err, ErrMissingConfig)
}

// ── Name ──────────────────────────────────────────────────────────────────────

func TestDNSPod_Name(t *testing.T) {
	p := newDNSPodTestProvider(t, func(w http.ResponseWriter, r *http.Request) {})
	assert.Equal(t, "dnspod", p.Name())
}

// ── ListRecords ───────────────────────────────────────────────────────────────

func TestDNSPod_ListRecords_HappyPath(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DescribeRecordList", readActionFromRequest(r))
		body := readBodyAsMap(t, r)
		assert.Equal(t, "example.com", body["Domain"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": "req-list",
			"RecordList": []map[string]any{
				{"RecordId": 1001, "Name": "www", "Type": "A", "Value": "1.2.3.4", "TTL": 300, "MX": 0, "Line": "默认", "Status": "ENABLE"},
				{"RecordId": 1002, "Name": "@", "Type": "MX", "Value": "mail.example.com", "TTL": 600, "MX": 10, "Line": "默认", "Status": "ENABLE"},
			},
			"RecordCountInfo": map[string]any{"TotalCount": 2, "ListedCount": 2},
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	records, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 2)

	assert.Equal(t, "1001", records[0].ID)
	assert.Equal(t, "A", records[0].Type)
	assert.Equal(t, "www.example.com", records[0].Name)
	assert.Equal(t, "1.2.3.4", records[0].Content)
	assert.Equal(t, 300, records[0].TTL)

	assert.Equal(t, "1002", records[1].ID)
	assert.Equal(t, "MX", records[1].Type)
	assert.Equal(t, "example.com", records[1].Name) // "@" → bare domain
	assert.Equal(t, 10, records[1].Priority)
}

func TestDNSPod_ListRecords_FilterType(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body := readBodyAsMap(t, r)
		assert.Equal(t, "TXT", body["RecordType"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId":       "req-filter-type",
			"RecordList":      []map[string]any{},
			"RecordCountInfo": map[string]any{"TotalCount": 0, "ListedCount": 0},
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	records, err := p.ListRecords(t.Context(), "", RecordFilter{Type: "TXT"})
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestDNSPod_ListRecords_FilterName(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body := readBodyAsMap(t, r)
		assert.Equal(t, "api", body["Subdomain"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId":       "req-filter-name",
			"RecordList":      []map[string]any{},
			"RecordCountInfo": map[string]any{"TotalCount": 0, "ListedCount": 0},
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	records, err := p.ListRecords(t.Context(), "", RecordFilter{Name: "api.example.com"})
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestDNSPod_ListRecords_Pagination(t *testing.T) {
	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body := readBodyAsMap(t, r)
		offset := int(body["Offset"].(float64))
		limit := int(body["Limit"].(float64))
		assert.EqualValues(t, 300, limit)

		var records []map[string]any
		var total int
		var listed int

		switch offset {
		case 0:
			// First page: return 300 records
			total = 350
			listed = 300
			for i := 0; i < 300; i++ {
				records = append(records, map[string]any{
					"RecordId": uint64(1000 + i),
					"Name":     fmt.Sprintf("sub%d", i),
					"Type":     "A", "Value": "1.1.1.1", "TTL": 300, "MX": 0, "Line": "默认",
				})
			}
		case 300:
			// Second page: return remaining 50
			total = 350
			listed = 50
			for i := 0; i < 50; i++ {
				records = append(records, map[string]any{
					"RecordId": uint64(2000 + i),
					"Name":     fmt.Sprintf("extra%d", i),
					"Type":     "A", "Value": "2.2.2.2", "TTL": 300, "MX": 0, "Line": "默认",
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId":  fmt.Sprintf("req-page-%d", offset),
			"RecordList": records,
			"RecordCountInfo": map[string]any{
				"TotalCount":  total,
				"ListedCount": listed,
			},
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	records, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.NoError(t, err)
	assert.Len(t, records, 350)
	assert.Equal(t, 2, callCount)
}

func TestDNSPod_ListRecords_UsesConfiguredZoneWhenEmpty(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body := readBodyAsMap(t, r)
		assert.Equal(t, "example.com", body["Domain"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId":       "req-zone",
			"RecordList":      []map[string]any{},
			"RecordCountInfo": map[string]any{"TotalCount": 0, "ListedCount": 0},
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.ListRecords(t.Context(), "", RecordFilter{}) // empty zone → falls back to "example.com"
	require.NoError(t, err)
}

func TestDNSPod_ListRecords_UsesExplicitZone(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body := readBodyAsMap(t, r)
		assert.Equal(t, "other.com", body["Domain"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId":       "req-explicit-zone",
			"RecordList":      []map[string]any{},
			"RecordCountInfo": map[string]any{"TotalCount": 0, "ListedCount": 0},
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.ListRecords(t.Context(), "other.com", RecordFilter{})
	require.NoError(t, err)
}

func TestDNSPod_ListRecords_EmptyList(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId":       "req-empty",
			"RecordList":      []map[string]any{},
			"RecordCountInfo": map[string]any{"TotalCount": 0, "ListedCount": 0},
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	records, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestDNSPod_ListRecords_APIError(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodErrorResponse(t, "InvalidParameter.DomainNotExist", "domain not exist"))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.ErrorIs(t, err, ErrZoneNotFound)
}

// ── CreateRecord ──────────────────────────────────────────────────────────────

func TestDNSPod_CreateRecord_HappyPath(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "CreateRecord", readActionFromRequest(r))
		body := readBodyAsMap(t, r)
		assert.Equal(t, "example.com", body["Domain"])
		assert.Equal(t, "www", body["SubDomain"])
		assert.Equal(t, "A", body["RecordType"])
		assert.Equal(t, dnspodDefaultLine, body["RecordLine"])
		assert.Equal(t, "1.2.3.4", body["Value"])
		assert.EqualValues(t, 300, body["TTL"])

		recID := uint64(9001)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": "req-create",
			"RecordId":  recID,
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	created, err := p.CreateRecord(t.Context(), "", Record{
		Type: "A", Name: "www.example.com", Content: "1.2.3.4", TTL: 300,
	})
	require.NoError(t, err)
	assert.Equal(t, "9001", created.ID)
	assert.Equal(t, "www.example.com", created.Name)
	assert.Equal(t, "1.2.3.4", created.Content)
}

func TestDNSPod_CreateRecord_MXPriority(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body := readBodyAsMap(t, r)
		assert.Equal(t, "MX", body["RecordType"])
		assert.EqualValues(t, 20, body["MX"])

		recID := uint64(9002)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": "req-mx",
			"RecordId":  recID,
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	created, err := p.CreateRecord(t.Context(), "", Record{
		Type: "MX", Name: "example.com", Content: "mail.example.com", TTL: 300, Priority: 20,
	})
	require.NoError(t, err)
	assert.Equal(t, "9002", created.ID)
	assert.Equal(t, 20, created.Priority)
}

func TestDNSPod_CreateRecord_DefaultLine(t *testing.T) {
	// The RecordLine must always be "默认" regardless of caller input
	handler := func(w http.ResponseWriter, r *http.Request) {
		body := readBodyAsMap(t, r)
		assert.Equal(t, "默认", body["RecordLine"])

		recID := uint64(9003)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": "req-line",
			"RecordId":  recID,
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.CreateRecord(t.Context(), "", Record{
		Type: "TXT", Name: "verify.example.com", Content: "challenge-token", TTL: 60,
	})
	require.NoError(t, err)
}

func TestDNSPod_CreateRecord_MissingRecordID(t *testing.T) {
	// Response is success but omits RecordId — should return an error
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": "req-no-id",
			// RecordId intentionally absent
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.CreateRecord(t.Context(), "", Record{Type: "A", Name: "test.example.com", Content: "1.1.1.1", TTL: 300})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing RecordId")
}

func TestDNSPod_CreateRecord_AlreadyExists(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodErrorResponse(t, "InvalidParameter.RecordExistByRecordType", "record already exists"))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.CreateRecord(t.Context(), "", Record{Type: "A", Name: "dup.example.com", Content: "1.1.1.1", TTL: 300})
	require.ErrorIs(t, err, ErrRecordAlreadyExists)
}

// ── UpdateRecord ──────────────────────────────────────────────────────────────

func TestDNSPod_UpdateRecord_HappyPath(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "ModifyRecord", readActionFromRequest(r))
		body := readBodyAsMap(t, r)
		assert.Equal(t, "example.com", body["Domain"])
		assert.EqualValues(t, 5001, body["RecordId"])
		assert.Equal(t, "api", body["SubDomain"])
		assert.Equal(t, "A", body["RecordType"])
		assert.Equal(t, dnspodDefaultLine, body["RecordLine"])
		assert.Equal(t, "5.6.7.8", body["Value"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": "req-update",
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	updated, err := p.UpdateRecord(t.Context(), "", "5001", Record{
		Type: "A", Name: "api.example.com", Content: "5.6.7.8", TTL: 300,
	})
	require.NoError(t, err)
	assert.Equal(t, "5001", updated.ID)
	assert.Equal(t, "api.example.com", updated.Name)
}

func TestDNSPod_UpdateRecord_InvalidID(t *testing.T) {
	p := newDNSPodTestProvider(t, func(w http.ResponseWriter, r *http.Request) {})
	_, err := p.UpdateRecord(t.Context(), "", "not-a-number", Record{
		Type: "A", Name: "api.example.com", Content: "1.1.1.1", TTL: 300,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid record ID")
}

func TestDNSPod_UpdateRecord_NotFound(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodErrorResponse(t, "ResourceNotFound.NoDataOfRecord", "record not found"))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.UpdateRecord(t.Context(), "", "9999", Record{Type: "A", Name: "test.example.com", Content: "1.1.1.1", TTL: 300})
	require.ErrorIs(t, err, ErrRecordNotFound)
}

// ── DeleteRecord ──────────────────────────────────────────────────────────────

func TestDNSPod_DeleteRecord_HappyPath(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DeleteRecord", readActionFromRequest(r))
		body := readBodyAsMap(t, r)
		assert.Equal(t, "example.com", body["Domain"])
		assert.EqualValues(t, 7001, body["RecordId"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": "req-delete",
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	err := p.DeleteRecord(t.Context(), "", "7001")
	require.NoError(t, err)
}

func TestDNSPod_DeleteRecord_InvalidID(t *testing.T) {
	p := newDNSPodTestProvider(t, func(w http.ResponseWriter, r *http.Request) {})
	err := p.DeleteRecord(t.Context(), "", "bad-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid record ID")
}

func TestDNSPod_DeleteRecord_NotFound(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodErrorResponse(t, "InvalidParameter.RecordIdInvalid", "record id invalid"))
	}

	p := newDNSPodTestProvider(t, handler)
	err := p.DeleteRecord(t.Context(), "", "4242")
	require.ErrorIs(t, err, ErrRecordNotFound)
}

// ── GetNameservers ────────────────────────────────────────────────────────────

func TestDNSPod_GetNameservers_HappyPath(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DescribeDomain", readActionFromRequest(r))
		body := readBodyAsMap(t, r)
		assert.Equal(t, "example.com", body["Domain"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": "req-ns",
			"DomainInfo": map[string]any{
				"Domain":       "example.com",
				"DomainId":     12345,
				"EffectiveDNS": []string{"ns1.dnspod.net", "ns2.dnspod.net"},
			},
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	ns, err := p.GetNameservers(t.Context(), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"ns1.dnspod.net", "ns2.dnspod.net"}, ns)
}

func TestDNSPod_GetNameservers_NoInfo(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// DomainInfo absent
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": "req-ns-nil",
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.GetNameservers(t.Context(), "")
	require.ErrorIs(t, err, ErrZoneNotFound)
}

func TestDNSPod_GetNameservers_EmptyEffectiveDNS(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": "req-ns-empty",
			"DomainInfo": map[string]any{
				"Domain":       "example.com",
				"DomainId":     12345,
				"EffectiveDNS": []string{},
			},
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.GetNameservers(t.Context(), "")
	require.ErrorIs(t, err, ErrZoneNotFound)
}

func TestDNSPod_GetNameservers_ZoneNotFound(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodErrorResponse(t, "ResourceNotFound.NoDataOfDomain", "domain not found"))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.GetNameservers(t.Context(), "")
	require.ErrorIs(t, err, ErrZoneNotFound)
}

// ── BatchCreateRecords ────────────────────────────────────────────────────────

func TestDNSPod_BatchCreateRecords_HappyPath(t *testing.T) {
	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		recID := uint64(8000 + callCount)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": fmt.Sprintf("req-batch-%d", callCount),
			"RecordId":  recID,
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	records := []Record{
		{Type: "A", Name: "a.example.com", Content: "1.1.1.1", TTL: 300},
		{Type: "A", Name: "b.example.com", Content: "2.2.2.2", TTL: 300},
		{Type: "CNAME", Name: "www.example.com", Content: "a.example.com", TTL: 300},
	}
	created, err := p.BatchCreateRecords(t.Context(), "", records)
	require.NoError(t, err)
	assert.Len(t, created, 3)
	assert.Equal(t, 3, callCount)
}

func TestDNSPod_BatchCreateRecords_StopsOnFirstError(t *testing.T) {
	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(dnspodErrorResponse(t, "InvalidParameter.RecordExistByRecordType", "already exists"))
			return
		}
		recID := uint64(8100 + callCount)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": fmt.Sprintf("req-batch-err-%d", callCount),
			"RecordId":  recID,
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	records := []Record{
		{Type: "A", Name: "ok.example.com", Content: "1.1.1.1", TTL: 300},
		{Type: "A", Name: "dup.example.com", Content: "2.2.2.2", TTL: 300},
		{Type: "A", Name: "never.example.com", Content: "3.3.3.3", TTL: 300},
	}
	created, err := p.BatchCreateRecords(t.Context(), "", records)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRecordAlreadyExists)
	assert.Len(t, created, 1)      // only first succeeded
	assert.Equal(t, 2, callCount)  // third call never made
}

// ── BatchDeleteRecords ────────────────────────────────────────────────────────

func TestDNSPod_BatchDeleteRecords_HappyPath(t *testing.T) {
	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": fmt.Sprintf("req-del-batch-%d", callCount),
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	err := p.BatchDeleteRecords(t.Context(), "", []string{"101", "102", "103"})
	require.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestDNSPod_BatchDeleteRecords_StopsOnError(t *testing.T) {
	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(dnspodErrorResponse(t, "ResourceNotFound.NoDataOfRecord", "not found"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId": fmt.Sprintf("req-del-err-%d", callCount),
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	err := p.BatchDeleteRecords(t.Context(), "", []string{"201", "202", "203"})
	require.ErrorIs(t, err, ErrRecordNotFound)
	assert.Equal(t, 2, callCount)
}

// ── dnspodCheckHTTP table-driven ──────────────────────────────────────────────

func TestDNSPodCheckHTTP(t *testing.T) {
	cases := []struct {
		name    string
		code    int
		body    []byte
		wantNil bool
		wantErr error
	}{
		{"200 ok", 200, nil, true, nil},
		{"401 unauthorized", 401, nil, false, ErrUnauthorized},
		{"403 forbidden", 403, nil, false, ErrUnauthorized},
		{"429 rate limit", 429, nil, false, ErrRateLimitExceeded},
		{"500 server error", 500, []byte("internal server error"), false, nil},
		{"503 service unavailable", 503, []byte("down"), false, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := dnspodCheckHTTP(tc.code, tc.body)
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

// ── dnspodMapCode table-driven ────────────────────────────────────────────────

func TestDNSPodMapCode(t *testing.T) {
	cases := []struct {
		code    string
		wantErr error
	}{
		{"AuthFailure", ErrUnauthorized},
		{"AuthFailure.SignatureFailure", ErrUnauthorized},
		{"AuthFailure.TokenFailure", ErrUnauthorized},
		{"AuthFailure.SecretIdNotFound", ErrUnauthorized},
		{"AuthFailure.InvalidSecretId", ErrUnauthorized},
		{"ResourceNotFound.NoDataOfRecord", ErrRecordNotFound},
		{"InvalidParameter.RecordIdInvalid", ErrRecordNotFound},
		{"InvalidParameter.DomainNotExist", ErrZoneNotFound},
		{"ResourceNotFound.NoDataOfDomain", ErrZoneNotFound},
		{"LimitExceeded.Freq", ErrRateLimitExceeded},
		{"RequestLimitExceeded", ErrRateLimitExceeded},
		{"LimitExceeded", ErrRateLimitExceeded},
		{"InvalidParameter.RecordExistByRecordType", ErrRecordAlreadyExists},
		{"UnknownError", nil}, // falls through to generic error
	}

	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			err := dnspodMapCode(tc.code, "test message")
			require.Error(t, err)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr, "expected sentinel for code %q", tc.code)
			} else {
				assert.Contains(t, err.Error(), tc.code)
				assert.Contains(t, err.Error(), "test message")
			}
		})
	}
}

// ── HTTP transport errors ─────────────────────────────────────────────────────

func TestDNSPod_CallReturnsErrorOnBadJSON(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not-json`))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse response")
}

func TestDNSPod_CallReturnsErrorOnHTTPFailure(t *testing.T) {
	// Point provider at a closed server to simulate a connection-refused error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	savedURL := srv.URL
	srv.Close() // close immediately so the client gets a connection error

	p := newDNSPodProviderWithClient("example.com", "ID", "Key", savedURL, srv.Client())
	_, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.Error(t, err)
}

// ── Registry ──────────────────────────────────────────────────────────────────

func TestRegistry_DNSPodRegistered(t *testing.T) {
	cfg := json.RawMessage(`{"domain_name":"example.com"}`)
	creds := json.RawMessage(`{"secret_id":"AKIDtest","secret_key":"keysecret"}`)
	p, err := Get("dnspod", cfg, creds)
	require.NoError(t, err)
	assert.Equal(t, "dnspod", p.Name())
}

// ── dnspodToRecord ────────────────────────────────────────────────────────────

func TestDNSPodToRecord_AtSign(t *testing.T) {
	r := dnspodRecordItem{
		RecordId: 42, Name: "@", Type: "A", Value: "9.9.9.9", TTL: 600,
	}
	rec := dnspodToRecord(r, "example.com")
	assert.Equal(t, "example.com", rec.Name)
	assert.Equal(t, "42", rec.ID)
}

func TestDNSPodToRecord_SubdomainPart(t *testing.T) {
	r := dnspodRecordItem{
		RecordId: 55, Name: "api", Type: "CNAME", Value: "backend.example.com", TTL: 300,
	}
	rec := dnspodToRecord(r, "example.com")
	assert.Equal(t, "api.example.com", rec.Name)
	assert.Equal(t, "CNAME", rec.Type)
}

func TestDNSPodToRecord_MXPriority(t *testing.T) {
	r := dnspodRecordItem{
		RecordId: 99, Name: "@", Type: "MX", Value: "mail.example.com", TTL: 300, MX: 15,
	}
	rec := dnspodToRecord(r, "example.com")
	assert.Equal(t, 15, rec.Priority)
	assert.Equal(t, "example.com", rec.Name)
}

// ── Header signing is verified indirectly (server checks X-TC-Action) ─────────

func TestDNSPod_RequestContainsRequiredHeaders(t *testing.T) {
	var capturedHeaders http.Header
	handler := func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(dnspodSuccessResponse(t, map[string]any{
			"RequestId":       "req-headers",
			"RecordList":      []map[string]any{},
			"RecordCountInfo": map[string]any{"TotalCount": 0, "ListedCount": 0},
		}))
	}

	p := newDNSPodTestProvider(t, handler)
	_, err := p.ListRecords(t.Context(), "", RecordFilter{})
	require.NoError(t, err)

	assert.NotEmpty(t, capturedHeaders.Get("Authorization"))
	assert.Equal(t, "application/json", capturedHeaders.Get("Content-Type"))
	assert.Equal(t, "DescribeRecordList", capturedHeaders.Get("X-TC-Action"))
	assert.Equal(t, dnspodVersion, capturedHeaders.Get("X-TC-Version"))
	assert.NotEmpty(t, capturedHeaders.Get("X-TC-Timestamp"))
	assert.True(t,
		strings.HasPrefix(capturedHeaders.Get("Authorization"), "TC3-HMAC-SHA256 "),
		"Authorization should use TC3-HMAC-SHA256 scheme",
	)
}
